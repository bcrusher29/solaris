package library

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cespare/xxhash"
	"github.com/karrick/godirwalk"

	"github.com/bcrusher29/solaris/config"
	"github.com/bcrusher29/solaris/database"
	"github.com/bcrusher29/solaris/playcount"
	"github.com/bcrusher29/solaris/tmdb"
	"github.com/bcrusher29/solaris/trakt"
	"github.com/bcrusher29/solaris/util"
	"github.com/bcrusher29/solaris/xbmc"
)

var (
	movieRegexp = regexp.MustCompile(`^plugin://plugin.video.elementum.*/movie/\w+/(\d+)`)
	showRegexp  = regexp.MustCompile(`^plugin://plugin.video.elementum.*/show/\w+/(\d+)/(\d+)/(\d+)`)
)

// RefreshOnScan is launched when scan is finished
func RefreshOnScan() error {
	return nil
}

// RefreshOnClean is launched when clear is finished
func RefreshOnClean() error {
	if !tmdb.WarmingUp.IsSet() || !initialized {
		return nil
	}

	ClearResolveCache()

	rows, err := database.Get().Query(`SELECT tmdbId, mediaType, showID FROM library_items WHERE state = ?`, StateActive)
	if err != nil {
		log.Errorf("Cannot fetch library items: %s", err)
		return err
	}

	qMovies := []int{}
	qShows := []int{}

	tmdbID := 0
	showID := 0
	mediaType := 0
	for rows.Next() {
		rows.Scan(&tmdbID, &mediaType, &showID)

		if mediaType == MovieType {
			if _, err := GetMovieByTMDB(tmdbID); err != nil {
				qMovies = append(qMovies, tmdbID)
			}
		} else if mediaType == ShowType {
			if _, err := GetShowByTMDB(tmdbID); err != nil {
				qShows = append(qShows, tmdbID)
			}
		}
	}
	rows.Close()

	if len(qMovies) > 0 && len(qShows) > 0 {
		tx, err := database.Get().Begin()
		if err != nil {
			log.Debugf("Cannot start transaction: %s", err)
		}
		for _, id := range qMovies {
			_, err := tx.Exec(`UPDATE library_items SET state = ? WHERE tmdbId = ? AND mediaType = ?`, StateDeleted, id, MovieType)
			if err != nil {
				log.Debugf("updateDBItem failed: %s", err)
				tx.Rollback()
				break
			}
		}
		for _, id := range qShows {
			_, err := tx.Exec(`UPDATE library_items SET state = ? WHERE tmdbId = ? AND mediaType = ?`, StateDeleted, id, ShowType)
			if err != nil {
				log.Debugf("updateDBItem failed: %s", err)
				tx.Rollback()
				break
			}
		}

		tx.Commit()
		log.Debugf("Finished cleaning up %d movies and %d shows", len(qMovies), len(qShows))
	}

	Refresh()
	ClearPageCache()

	xbmc.Refresh()

	return nil
}

// Refresh is updating library from Kodi
func Refresh() error {
	if TraktScanning {
		return nil
	}
	defer util.FreeMemoryGC()

	if err := RefreshMovies(); err != nil {
		log.Debugf("RefreshMovies got an error: %v", err)
	}
	if err := RefreshShows(); err != nil {
		log.Debugf("RefreshShows got an error: %v", err)
	}

	log.Debug("Library refresh finished")

	return nil
}

// RefreshMovies updates movies in the library
func RefreshMovies() error {
	started := time.Now()

	if Scanning {
		return nil
	}

	Scanning = true
	defer func() {
		Scanning = false
		RefreshUIDs()
	}()

	var movies *xbmc.VideoLibraryMovies
	for tries := 1; tries <= 3; tries++ {
		var err error
		movies, err = xbmc.VideoLibraryGetMovies()
		if movies == nil || err != nil {
			time.Sleep(time.Duration(tries*2) * time.Second)
			continue
		}

		break
	}

	if movies != nil && movies.Limits != nil && movies.Limits.Total == 0 {
		return nil
	}

	if movies == nil || movies.Movies == nil {
		return errors.New("Could not fetch Movies from Kodi")
	}

	defer func() {
		log.Debugf("Fetched %d movies from Kodi Library in %s", len(movies.Movies), time.Since(started))
	}()

	l.mu.Movies.Lock()
	defer l.mu.Movies.Unlock()

	l.Movies = map[int]*Movie{}
	for _, m := range movies.Movies {
		m.UniqueIDs.Kodi = m.ID
		if m.UniqueIDs.IMDB == "" && m.IMDBNumber != "" && strings.HasPrefix(m.IMDBNumber, "tt") {
			m.UniqueIDs.IMDB = m.IMDBNumber
		}

		l.Movies[m.ID] = &Movie{
			ID:       m.ID,
			Title:    m.Title,
			File:     m.File,
			Year:     m.Year,
			Resume:   &Resume{},
			UIDs:     &UniqueIDs{Kodi: m.ID, Playcount: m.PlayCount},
			XbmcUIDs: &m.UniqueIDs,
		}

		if m.Resume != nil {
			l.Movies[m.ID].Resume.Position = m.Resume.Position
			l.Movies[m.ID].Resume.Total = m.Resume.Total
		}
	}

	if err := database.Get().Ping(); err != nil {
		return err
	}

	getUIDsStmt, _ := database.Get().Prepare(`SELECT kodi, tmdb, tvdb, imdb, trakt FROM library_uids WHERE mediaType = ? AND kodi = ? LIMIT 1`)
	setUIDsStmt, _ := database.Get().Prepare(`INSERT OR REPLACE INTO library_uids (mediaType, kodi, tmdb, tvdb, imdb, trakt, playcount) VALUES (?, ?, ?, ?, ?, ?, ?)`)

	for _, m := range l.Movies {
		parseUniqueID(MovieType, m.UIDs, m.XbmcUIDs, m.File, m.Year, getUIDsStmt, setUIDsStmt)
	}

	return nil
}

// RefreshShows updates shows in the library
func RefreshShows() error {
	started := time.Now()

	if Scanning {
		return nil
	}

	Scanning = true
	defer func() {
		Scanning = false
		RefreshUIDs()
	}()

	var shows *xbmc.VideoLibraryShows
	for tries := 1; tries <= 3; tries++ {
		var err error
		shows, err = xbmc.VideoLibraryGetShows()
		if err != nil {
			time.Sleep(time.Duration(tries*500) * time.Millisecond)
			continue
		}
		break
	}

	if shows != nil && shows.Limits != nil && shows.Limits.Total == 0 {
		return nil
	}

	if shows == nil || shows.Shows == nil {
		return errors.New("Could not fetch Shows from Kodi")
	}

	defer func() {
		log.Debugf("Fetched %d shows from Kodi Library in %s", len(shows.Shows), time.Since(started))
	}()

	l.mu.Shows.Lock()
	defer l.mu.Shows.Unlock()

	l.Shows = map[int]*Show{}
	for _, s := range shows.Shows {
		s.UniqueIDs.Kodi = s.ID
		if s.UniqueIDs.IMDB == "" && s.IMDBNumber != "" && strings.HasPrefix(s.IMDBNumber, "tt") {
			s.UniqueIDs.IMDB = s.IMDBNumber
		}

		l.Shows[s.ID] = &Show{
			ID:       s.ID,
			Title:    s.Title,
			Seasons:  map[int]*Season{},
			Episodes: map[int]*Episode{},
			Year:     s.Year,
			UIDs:     &UniqueIDs{Kodi: s.ID, Playcount: s.PlayCount},
			XbmcUIDs: &s.UniqueIDs,
		}
	}

	if err := RefreshSeasons(); err != nil {
		log.Debugf("RefreshSeasons got an error: %v", err)
	}
	if err := RefreshEpisodes(); err != nil {
		log.Debugf("RefreshEpisodes got an error: %v", err)
	}

	if err := database.Get().Ping(); err != nil {
		return err
	}

	getUIDsStmt, _ := database.Get().Prepare(`SELECT kodi, tmdb, tvdb, imdb, trakt FROM library_uids WHERE mediaType = ? AND kodi = ? LIMIT 1`)
	setUIDsStmt, _ := database.Get().Prepare(`INSERT OR REPLACE INTO library_uids (mediaType, kodi, tmdb, tvdb, imdb, trakt, playcount) VALUES (?, ?, ?, ?, ?, ?, ?)`)

	// TODO: This needs refactor to avoid setting global Lock on processing,
	// should use temporary container to process and then sync to Shows
	for _, show := range l.Shows {
		// Step 1: try to get information from what we get from Kodi
		parseUniqueID(ShowType, show.UIDs, show.XbmcUIDs, "", show.Year, getUIDsStmt, setUIDsStmt)

		// Step 2: if TMDB not found - try to find it from episodes
		if show.UIDs.TMDB == 0 {
			for _, e := range show.Episodes {
				if !strings.HasSuffix(e.File, ".strm") {
					continue
				}

				u := &UniqueIDs{}
				parseUniqueID(EpisodeType, u, e.XbmcUIDs, e.File, 0, getUIDsStmt, setUIDsStmt)
				if u.TMDB != 0 {
					show.UIDs.TMDB = u.TMDB
					break
				}
			}
		}

		if show.UIDs.TMDB == 0 {
			continue
		}

		for _, s := range show.Seasons {
			s.UIDs.TMDB = show.UIDs.TMDB
			s.UIDs.TVDB = show.UIDs.TVDB
			s.UIDs.IMDB = show.UIDs.IMDB

			parseUniqueID(SeasonType, s.UIDs, s.XbmcUIDs, "", 0, getUIDsStmt, setUIDsStmt)
		}
		for _, e := range show.Episodes {
			e.UIDs.TMDB = show.UIDs.TMDB
			e.UIDs.TVDB = show.UIDs.TVDB
			e.UIDs.IMDB = show.UIDs.IMDB

			parseUniqueID(EpisodeType, e.UIDs, e.XbmcUIDs, "", 0, getUIDsStmt, setUIDsStmt)
		}
	}

	return nil
}

// RefreshSeasons updates seasons list for selected show in the library
func RefreshSeasons() error {
	started := time.Now()

	var seasons *xbmc.VideoLibrarySeasons

	// Collect all shows IDs for possibly doing one-by-one calls to Kodi
	shows := []int{}
	for _, s := range l.Shows {
		shows = append(shows, s.ID)
	}

	for tries := 1; tries <= 3; tries++ {
		var err error
		seasons, err = xbmc.VideoLibraryGetAllSeasons(shows)
		if seasons == nil || err != nil {
			time.Sleep(time.Duration(tries*500) * time.Millisecond)
			continue
		}
		break
	}

	if seasons == nil || seasons.Seasons == nil {
		return errors.New("Could not fetch Seasons from Kodi")
	}
	defer func() {
		log.Debugf("Fetched %d seasons from Kodi Library in %s", len(seasons.Seasons), time.Since(started))
	}()

	cleanupCheck := map[int]bool{}
	for _, s := range seasons.Seasons {
		if c, ok := l.Shows[s.TVShowID]; !ok || c == nil || c.Seasons == nil {
			continue
		}

		if _, ok := cleanupCheck[s.TVShowID]; !ok {
			l.Shows[s.TVShowID].Seasons = map[int]*Season{}
			cleanupCheck[s.TVShowID] = true
		}

		s.UniqueIDs.Kodi = s.ID

		l.Shows[s.TVShowID].Seasons[s.ID] = &Season{
			ID:       s.ID,
			Title:    s.Title,
			Season:   s.Season,
			Episodes: s.Episodes,
			UIDs:     &UniqueIDs{Kodi: s.ID, Playcount: s.PlayCount},
			XbmcUIDs: &s.UniqueIDs,
		}
	}

	return nil
}

// RefreshEpisodes updates episodes list for selected show in the library
func RefreshEpisodes() error {
	started := time.Now()

	var episodes *xbmc.VideoLibraryEpisodes
	for tries := 1; tries <= 3; tries++ {
		var err error
		episodes, err = xbmc.VideoLibraryGetAllEpisodes()
		if episodes == nil || err != nil {
			time.Sleep(time.Duration(tries*2) * time.Second)
			continue
		}
		break
	}

	if episodes == nil || episodes.Episodes == nil {
		return errors.New("Could not fetch Episodes from Kodi")
	}

	defer func() {
		log.Debugf("Fetched %d episodes from Kodi Library in %s", len(episodes.Episodes), time.Since(started))
	}()

	cleanupCheck := map[int]bool{}
	for _, e := range episodes.Episodes {
		if c, ok := l.Shows[e.TVShowID]; !ok || c == nil || c.Episodes == nil {
			continue
		}

		if _, ok := cleanupCheck[e.TVShowID]; !ok {
			l.Shows[e.TVShowID].Episodes = map[int]*Episode{}
			cleanupCheck[e.TVShowID] = true
		}

		e.UniqueIDs.Kodi = e.ID
		e.UniqueIDs.TMDB = ""
		e.UniqueIDs.TVDB = ""
		e.UniqueIDs.Trakt = ""
		e.UniqueIDs.Unknown = ""

		l.Shows[e.TVShowID].Episodes[e.ID] = &Episode{
			ID:       e.ID,
			Title:    e.Title,
			Season:   e.Season,
			Episode:  e.Episode,
			File:     e.File,
			Resume:   &Resume{},
			UIDs:     &UniqueIDs{Kodi: e.ID, Playcount: e.PlayCount},
			XbmcUIDs: &e.UniqueIDs,
		}

		if e.Resume != nil {
			l.Shows[e.TVShowID].Episodes[e.ID].Resume.Position = e.Resume.Position
			l.Shows[e.TVShowID].Episodes[e.ID].Resume.Total = e.Resume.Total
		}
	}

	return nil
}

// RefreshMovie ...
func RefreshMovie(kodiID, action int) {
	if action == ActionDelete || action == ActionSafeDelete {
		uids := GetUIDsFromKodi(kodiID)
		if uids == nil || uids.TMDB == 0 {
			return
		}

		if action == ActionDelete {
			if _, err := RemoveMovie(uids.TMDB); err != nil {
				log.Warning("Nothing left to remove from Elementum")
			}
		}

		l.mu.Movies.Lock()
		delete(l.Movies, kodiID)
		l.mu.Movies.Unlock()
	} else {
		RefreshMovies()
	}

	RefreshUIDs()
}

// RefreshShow ...
func RefreshShow(kodiID, action int) {
	if action == ActionDelete || action == ActionSafeDelete {
		uids := GetUIDsFromKodi(kodiID)
		if uids == nil || uids.TMDB == 0 {
			return
		}

		if action == ActionDelete {
			id := strconv.Itoa(uids.TMDB)
			if _, err := RemoveShow(id); err != nil {
				log.Warning("Nothing left to remove from Elementum")
			}
		}

		l.mu.Shows.Lock()
		delete(l.Shows, kodiID)
		l.mu.Shows.Unlock()
	} else {
		RefreshShows()
	}

	RefreshUIDs()
}

// RefreshEpisode ...
func RefreshEpisode(kodiID, action int) {
	if action != ActionDelete && action != ActionSafeDelete {
		return
	}

	s, e := GetLibraryEpisode(kodiID)
	if s == nil || e == nil {
		return
	}

	if action == ActionDelete {
		RemoveEpisode(e.UIDs.TMDB, s.UIDs.TMDB, e.Season, e.Episode)
	}

	l.mu.Shows.Lock()
	delete(l.Shows[s.UIDs.Kodi].Episodes, kodiID)
	l.mu.Shows.Unlock()

	RefreshUIDs()
}

// RefreshUIDs updates unique IDs for each library item
// This collects already saved UIDs for easier access
func RefreshUIDs() error {
	l.mu.UIDs.Lock()
	defer l.mu.UIDs.Unlock()

	playcount.Mu.Lock()
	defer playcount.Mu.Unlock()
	playcount.Watched = map[uint64]bool{}
	l.UIDs = map[uint64]*UniqueIDs{}
	for k, v := range l.WatchedTrakt {
		playcount.Watched[k] = v
	}

	for _, m := range l.Movies {
		m.UIDs.MediaType = MovieType
		id := xxhash.Sum64String(fmt.Sprintf("%d_%d", MovieType, m.UIDs.Kodi))
		l.UIDs[id] = m.UIDs

		if m.UIDs.Playcount > 0 {
			playcount.Watched[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", MovieType, TMDBScraper, m.UIDs.TMDB))] = true
			playcount.Watched[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", MovieType, TraktScraper, m.UIDs.Trakt))] = true
			playcount.Watched[xxhash.Sum64String(fmt.Sprintf("%d_%d_%s", MovieType, IMDBScraper, m.UIDs.IMDB))] = true
		}
	}
	for _, s := range l.Shows {
		s.UIDs.MediaType = ShowType
		id := xxhash.Sum64String(fmt.Sprintf("%d_%d", ShowType, s.UIDs.Kodi))
		l.UIDs[id] = s.UIDs

		if s.UIDs.Playcount > 0 {
			playcount.Watched[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", ShowType, TMDBScraper, s.UIDs.TMDB))] = true
			playcount.Watched[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", ShowType, TraktScraper, s.UIDs.Trakt))] = true
			playcount.Watched[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", ShowType, TVDBScraper, s.UIDs.TVDB))] = true
		}

		for _, e := range l.Shows[s.ID].Seasons {
			e.UIDs.MediaType = SeasonType
			id := xxhash.Sum64String(fmt.Sprintf("%d_%d", SeasonType, e.UIDs.Kodi))
			l.UIDs[id] = e.UIDs

			if e.UIDs.Playcount > 0 {
				playcount.Watched[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d", SeasonType, TMDBScraper, s.UIDs.TMDB, e.Season))] = true
				playcount.Watched[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d", SeasonType, TraktScraper, s.UIDs.Trakt, e.Season))] = true
				playcount.Watched[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d", SeasonType, TVDBScraper, s.UIDs.TVDB, e.Season))] = true
			}
		}
		for _, e := range l.Shows[s.ID].Episodes {
			e.UIDs.MediaType = EpisodeType
			id := xxhash.Sum64String(fmt.Sprintf("%d_%d", EpisodeType, e.UIDs.Kodi))
			l.UIDs[id] = e.UIDs

			if e.UIDs.Playcount > 0 {
				playcount.Watched[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d_%d", EpisodeType, TMDBScraper, s.UIDs.TMDB, e.Season, e.Episode))] = true
				playcount.Watched[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d_%d", EpisodeType, TraktScraper, s.UIDs.Trakt, e.Season, e.Episode))] = true
				playcount.Watched[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d_%d", EpisodeType, TVDBScraper, s.UIDs.TVDB, e.Season, e.Episode))] = true
			}
		}
	}

	log.Debugf("UIDs refresh finished")
	return nil
}

func parseUniqueID(entityType int, i *UniqueIDs, xbmcIDs *xbmc.UniqueIDs, fileName string, entityYear int, getUIDsStmt, setUIDsStmt *sql.Stmt) {
	err := getUIDsStmt.QueryRow(entityType, xbmcIDs.Kodi).Scan(&i.Kodi, &i.TMDB, &i.TVDB, &i.IMDB, &i.Trakt)
	if err != nil && err != sql.ErrNoRows {
		log.Debugf("Error getting UIDs from database: %s", err)
		return
	} else if err == nil && i.Kodi != 0 {
		tid := &UniqueIDs{}
		convertKodiIDsToLibrary(tid, xbmcIDs)

		if tid.TMDB != 0 && tid.TMDB == i.TMDB {
			return
		}
	}

	defer func() {
		setUIDsStmt.Exec(entityType, i.Kodi, i.TMDB, i.TVDB, i.IMDB, i.Trakt, i.Playcount)
	}()

	i.MediaType = entityType

	convertKodiIDsToLibrary(i, xbmcIDs)

	if i.TMDB != 0 {
		return
	}

	// If this is a strm file then we try to get TMDB id from it
	if len(fileName) > 0 {
		id, err := findTMDBInFile(fileName, xbmcIDs.Unknown)
		if id != 0 {
			i.TMDB = id
			return
		} else if err != nil {
			log.Debugf("Error reading TMDB ID from the file %s: %#v", fileName, err)
		}
	}

	// We should not query for each episode, has no sense,
	// since we need only TVShow ID to be resolved
	if entityType == EpisodeType {
		return
	}

	// If we get here - we have no TMDB, so try to resolve it
	if len(i.IMDB) != 0 {
		i.TMDB = findTMDBIDs(entityType, "imdb_id", i.IMDB)
		if i.TMDB != 0 {
			return
		}
	}
	if i.TVDB != 0 {
		i.TMDB = findTMDBIDs(entityType, "tvdb_id", strconv.Itoa(i.TVDB))
		if i.TMDB != 0 {
			return
		}
	}

	// We don't have any Named IDs, only 'Unknown' so let's try to fetch it
	if xbmcIDs.Unknown != "" {
		localID, _ := strconv.Atoi(xbmcIDs.Unknown)
		if localID == 0 {
			return
		}

		// Try to treat as it is a TMDB id inside of Unknown field
		if entityType == MovieType {
			m := tmdb.GetMovie(localID, config.Get().Language)
			if m != nil {
				dt, err := time.Parse("2006-01-02", m.FirstAirDate)
				if err != nil || dt.Year() == entityYear {
					i.TMDB = m.ID
					return
				}
			}
		} else if entityType == ShowType {
			s := tmdb.GetShow(localID, config.Get().Language)
			if s != nil {
				dt, err := time.Parse("2006-01-02", s.FirstAirDate)
				if err != nil || dt.Year() == entityYear {
					i.TMDB = s.ID
					return
				}
			}

			// If not found, try to search as TVDB id
			id := findTMDBIDsWithYear(ShowType, "tvdb_id", xbmcIDs.Unknown, entityYear)
			if id != 0 {
				i.TMDB = id
				return
			}
		}
	}

	return
}

func convertKodiIDsToLibrary(i *UniqueIDs, xbmcIDs *xbmc.UniqueIDs) {
	if i == nil || xbmcIDs == nil {
		return
	}

	i.Kodi = xbmcIDs.Kodi
	i.IMDB = xbmcIDs.IMDB
	i.TMDB, _ = strconv.Atoi(xbmcIDs.TMDB)
	i.TVDB, _ = strconv.Atoi(xbmcIDs.TVDB)
	i.Trakt, _ = strconv.Atoi(xbmcIDs.Trakt)

	// Checking alternative fields
	// 		TheMovieDB
	if i.TMDB == 0 && len(xbmcIDs.TheMovieDB) > 0 {
		i.TMDB, _ = strconv.Atoi(xbmcIDs.TheMovieDB)
	}

	// 		IMDB
	if len(xbmcIDs.Unknown) > 0 && strings.HasPrefix(xbmcIDs.Unknown, "tt") {
		i.IMDB = xbmcIDs.Unknown
	}
}

func findTMDBInFile(fileName string, pattern string) (id int, err error) {
	if len(fileName) == 0 || !strings.HasSuffix(fileName, ".strm") {
		return
	}

	// Let's cache file search, it's bad to do that, anyway,
	// but we check only .strm files and do that once per 2 weeks
	cacheKey := fmt.Sprintf("Resolve_File_%s", fileName)
	if err := cacheStore.Get(cacheKey, &id); err == nil {
		return id, nil
	}
	defer func() {
		if id == 0 {
			log.Debugf("Count not get ID from the file %s with pattern %s", fileName, pattern)
		}
		cacheStore.Set(cacheKey, id, resolveFileExpiration)
	}()

	if _, errStat := os.Stat(fileName); errStat != nil {
		return 0, errStat
	}

	fileContent, errRead := ioutil.ReadFile(fileName)
	if errRead != nil {
		return 0, errRead
	}

	// Dummy check. If Unknown is found in the strm file - we treat it as tmdb id
	if len(pattern) > 1 && bytes.Contains(fileContent, []byte("/"+pattern)) {
		id, _ = strconv.Atoi(pattern)
		return
	}

	// Reading the strm file and passing to a regexp to get TMDB ID
	// This can't be done with episodes, since it has Show ID and not Episode ID
	if matches := resolveRegexp.FindSubmatch(fileContent); len(matches) > 1 {
		id, _ = strconv.Atoi(string(matches[1]))
		return
	}

	return
}

func findTMDBIDsWithYear(entityType int, source string, id string, year int) int {
	results := tmdb.Find(id, source)
	reserveID := 0

	if results != nil {
		if entityType == MovieType && len(results.MovieResults) > 0 {
			for _, e := range results.MovieResults {
				dt, err := time.Parse("2006-01-02", e.FirstAirDate)
				if err != nil || year == 0 || dt.Year() == 0 {
					reserveID = e.ID
					continue
				}
				if dt.Year() == year {
					return e.ID
				}
			}
		} else if entityType == ShowType && len(results.TVResults) > 0 {
			for _, e := range results.TVResults {
				dt, err := time.Parse("2006-01-02", e.FirstAirDate)
				if err != nil || year == 0 || dt.Year() == 0 {
					reserveID = e.ID
					continue
				}
				if dt.Year() == year {
					return e.ID
				}
			}
		} else if entityType == EpisodeType && len(results.TVEpisodeResults) > 0 {
			for _, e := range results.TVEpisodeResults {
				dt, err := time.Parse("2006-01-02", e.FirstAirDate)
				if err != nil || year == 0 || dt.Year() == 0 {
					reserveID = e.ID
					continue
				}
				if dt.Year() == year {
					return e.ID
				}
			}
		}
	}

	return reserveID
}

func findTMDBIDs(entityType int, source string, id string) int {
	results := tmdb.Find(id, source)
	if results != nil {
		if entityType == MovieType && len(results.MovieResults) == 1 && results.MovieResults[0] != nil {
			return results.MovieResults[0].ID
		} else if entityType == ShowType && len(results.TVResults) == 1 && results.TVResults[0] != nil {
			return results.TVResults[0].ID
		} else if entityType == EpisodeType && len(results.TVEpisodeResults) == 1 && results.TVEpisodeResults[0] != nil {
			return results.TVEpisodeResults[0].ID
		}
	}

	return 0
}

func findTraktIDs(entityType int, source int, id string) (ids *trakt.IDs) {
	switch entityType {
	case MovieType:
		var r *trakt.Movie
		if source == TMDBScraper {
			r = trakt.GetMovieByTMDB(id)
		} else if source == TraktScraper {
			r = trakt.GetMovie(id)
		}
		if r != nil && r.IDs != nil {
			ids = r.IDs
		}
	case ShowType:
		var r *trakt.Show
		if source == TMDBScraper {
			r = trakt.GetShowByTMDB(id)
		} else if source == TraktScraper {
			r = trakt.GetShow(id)
		}
		if r != nil && r.IDs != nil {
			ids = r.IDs
		}
	case EpisodeType:
		var r *trakt.Episode
		if source == TMDBScraper {
			r = trakt.GetEpisodeByTMDB(id)
		} else if source == TraktScraper {
			r = trakt.GetEpisodeByID(id)
		}
		if r != nil && r.IDs != nil {
			ids = r.IDs
		}
	}

	return
}

// RefreshLocal checks media directory to save up-to-date strm library
func RefreshLocal() error {
	if Scanning {
		return nil
	}

	Scanning = true
	defer func() {
		Scanning = false
	}()

	refreshLocalMovies()
	refreshLocalShows()

	return nil
}

func refreshLocalMovies() {
	moviesLibraryPath := MoviesLibraryPath()
	if _, err := os.Stat(moviesLibraryPath); err != nil {
		return
	}

	begin := time.Now()
	addon := []byte(config.Get().Info.ID)
	files := searchStrm(moviesLibraryPath)
	IDs := []int{}
	for _, f := range files {
		fileContent, err := ioutil.ReadFile(f)
		if err != nil || len(fileContent) == 0 || bytes.Index(fileContent, addon) < 0 {
			continue
		}

		if matches := movieRegexp.FindSubmatch(fileContent); len(matches) > 1 {
			id, _ := strconv.Atoi(string(matches[1]))
			IDs = append(IDs, id)
		}
	}

	if len(IDs) == 0 {
		return
	}

	getStmt, _ := database.Get().Prepare(`SELECT 1 FROM library_items WHERE tmdbId = ? AND mediaType = ? AND state = ? LIMIT 1`)
	setStmt, _ := database.Get().Prepare(`INSERT OR IGNORE INTO library_items (tmdbId, mediaType, state) VALUES (?, ?, ?)`)
	rid := 0
	for _, id := range IDs {
		if err := getStmt.QueryRow(id, MovieType, StateActive).Scan(&rid); err != nil && err == sql.ErrNoRows {
			setStmt.Exec(id, MovieType, StateActive)
		}
	}
	log.Debugf("Finished updating %d local movies in %s", len(IDs), time.Since(begin))

	return
}

func refreshLocalShows() {
	showsLibraryPath := ShowsLibraryPath()
	if _, err := os.Stat(showsLibraryPath); err != nil {
		return
	}

	begin := time.Now()
	addon := []byte(config.Get().Info.ID)
	files := searchStrm(showsLibraryPath)
	IDs := map[int]bool{}
	for _, f := range files {
		fileContent, err := ioutil.ReadFile(f)
		if err != nil || len(fileContent) == 0 || bytes.Index(fileContent, addon) < 0 {
			continue
		}

		if matches := showRegexp.FindSubmatch(fileContent); len(matches) > 1 {
			showID, _ := strconv.Atoi(string(matches[1]))
			IDs[showID] = true
		}
	}

	if len(IDs) == 0 {
		return
	}

	getStmt, _ := database.Get().Prepare(`SELECT 1 FROM library_items WHERE tmdbId = ? AND mediaType = ? AND state = ? LIMIT 1`)
	setStmt, _ := database.Get().Prepare(`INSERT OR IGNORE INTO library_items (tmdbId, mediaType, state) VALUES (?, ?, ?)`)
	rid := 0
	for id := range IDs {
		if err := getStmt.QueryRow(id, ShowType, StateActive).Scan(&rid); err != nil && err == sql.ErrNoRows {
			setStmt.Exec(id, ShowType, StateActive)
		}
	}
	log.Debugf("Finished updating %d local shows in %s", len(IDs), time.Since(begin))

	return
}

func searchStrm(dir string) []string {
	ret := []string{}

	godirwalk.Walk(dir, &godirwalk.Options{
		FollowSymbolicLinks: true,
		Callback: func(osPathname string, de *godirwalk.Dirent) error {
			if strings.HasSuffix(osPathname, ".strm") {
				ret = append(ret, osPathname)
			}
			return nil
		},
	})

	return ret
}
