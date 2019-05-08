package library

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cespare/xxhash"
	"github.com/op/go-logging"

	"github.com/bcrusher29/solaris/cache"
	"github.com/bcrusher29/solaris/config"
	"github.com/bcrusher29/solaris/database"
	"github.com/bcrusher29/solaris/tmdb"
	"github.com/bcrusher29/solaris/trakt"
	"github.com/bcrusher29/solaris/util"
	"github.com/bcrusher29/solaris/xbmc"
)

const (
	movieType   = "movie"
	showType    = "show"
	episodeType = "episode"

	trueType  = "true"
	falseType = "false"

	resolveExpiration     = 7 * 24 * time.Hour
	resolveFileExpiration = 60 * 24 * time.Hour
)

const (
	// MovieType ...
	MovieType = iota
	// ShowType ...
	ShowType
	// SeasonType ...
	SeasonType
	// EpisodeType ...
	EpisodeType
)

const (
	// StateDeleted ...
	StateDeleted = iota
	// StateActive ...
	StateActive
)

const (
	// ActionUpdate ...
	ActionUpdate = iota
	// ActionDelete ...
	ActionDelete
	// ActionSafeDelete ...
	ActionSafeDelete
)

const (
	// TVDBScraper ...
	TVDBScraper = iota
	// TMDBScraper ...
	TMDBScraper
	// TraktScraper ...
	TraktScraper
	// IMDBScraper ...
	IMDBScraper
)

const (
	// Active ...
	Active = iota
	// Deleted ...
	Deleted
)
const (
	// Delete ...
	Delete = iota
	// Update ...
	Update
	// Batch ...
	Batch
	// BatchDelete ...
	BatchDelete
	// DeleteTorrent ...
	DeleteTorrent
)

var (
	removedEpisodes = make(chan *removedEpisode)
	closer          = util.Event{}

	log = logging.MustGetLogger("library")

	cacheStore *cache.DBStore

	// Scanning shows if Kodi library Scan is in progress
	Scanning = false
	// TraktScanning shows if Trakt is working
	TraktScanning = false

	initialized = false

	resolveRegexp = regexp.MustCompile(`^plugin://plugin.video.elementum.*?(\d+)(\W|$)`)
)

var l = &Library{
	UIDs:   map[uint64]*UniqueIDs{},
	Movies: map[int]*Movie{},
	Shows:  map[int]*Show{},

	WatchedTrakt: map[uint64]bool{},
}

// InitDB ...
func InitDB() {
	cacheStore = cache.NewDBStore()
}

// Get returns singleton instance for Library
func Get() *Library {
	return l
}

// Init makes preparations on program start
func Init() {
	InitDB()

	if err := checkMoviesPath(); err != nil {
		xbmc.Notify("Elementum", err.Error(), config.AddonIcon())
		return
	}
	if err := checkShowsPath(); err != nil {
		xbmc.Notify("Elementum", err.Error(), config.AddonIcon())
		return
	}

	go func() {
		// Give time to Kodi to start its JSON-RPC service
		time.Sleep(5 * time.Second)

		RefreshLocal()
		Refresh()
		initialized = true
	}()

	// Removed episodes debouncer
	go func() {
		var episodes []*removedEpisode

		closing := closer.C()
		timer := time.NewTicker(3 * time.Second)
		defer timer.Stop()
		defer close(removedEpisodes)

		for {
			select {
			case <-closing:
				return

			case <-timer.C:
				if len(episodes) == 0 {
					break
				}

				shows := make(map[string][]*removedEpisode, 0)
				for _, episode := range episodes {
					shows[episode.ShowName] = append(shows[episode.ShowName], episode)
				}

				var label string
				var labels []string
				if len(episodes) > 1 {
					for showName, showEpisodes := range shows {
						var libraryTotal int
						if l.Shows == nil {
							break
						}
						show, err := GetShowByTMDB(showEpisodes[0].ShowID)
						if show != nil && err == nil {
							libraryTotal = len(show.Episodes)
						}
						if libraryTotal == 0 {
							break
						}
						if len(showEpisodes) == libraryTotal {
							ID := strconv.Itoa(showEpisodes[0].ShowID)
							if _, err := RemoveShow(ID); err != nil {
								log.Error("Unable to remove show after removing all episodes...")
							}
						} else {
							labels = append(labels, fmt.Sprintf("%d episodes of %s", len(showEpisodes), showName))
						}

						// Add single episodes to removed prefix
						var tmdbIDs []int
						for _, showEpisode := range showEpisodes {
							tmdbIDs = append(tmdbIDs, showEpisode.ID)
						}
						if err := updateBatchDBItem(tmdbIDs, StateDeleted, EpisodeType, showEpisodes[0].ShowID); err != nil {
							log.Error(err)
						}
					}
					if len(labels) > 0 {
						label = strings.Join(labels, ", ")
						if xbmc.DialogConfirmFocused("Elementum", fmt.Sprintf("LOCALIZE[30278];;%s", label)) {
							xbmc.VideoLibraryClean()
						}
					}
				} else {
					for showName, episode := range shows {
						label = fmt.Sprintf("%s S%02dE%02d", showName, episode[0].Season, episode[0].Episode)
						if err := updateDBItem(episode[0].ID, StateDeleted, EpisodeType, episode[0].ShowID); err != nil {
							log.Error(err)
						}
					}
					if xbmc.DialogConfirmFocused("Elementum", fmt.Sprintf("LOCALIZE[30278];;%s", label)) {
						xbmc.VideoLibraryClean()
					}
				}

				episodes = make([]*removedEpisode, 0)

			case episode, ok := <-removedEpisodes:
				if !ok {
					break
				}
				episodes = append(episodes, episode)
			}
		}
	}()

	updateDelay := config.Get().UpdateDelay
	if updateDelay > 0 {
		if updateDelay < 10 {
			// Give time to Elementum to update its cache of libraryMovies, libraryShows and libraryEpisodes
			updateDelay = 10
		}
		go func() {
			time.Sleep(time.Duration(updateDelay) * time.Second)
			closing := closer.C()

			select {
			case <-closing:
				return
			default:
				go func() {
					if config.Get().UpdateFrequency > 0 {
						if err := doUpdateLibrary(); err != nil {
							log.Warning(err)
						}
						if config.Get().UpdateAutoScan && Scanning == false {
							Scanning = true
							xbmc.VideoLibraryScan()
							Scanning = false
						}
					}

					if config.Get().TraktSyncFrequency > 0 && config.Get().TraktToken != "" {
						if err := RefreshTrakt(); err != nil {
							log.Warning(err)
						}
						if config.Get().UpdateAutoScan && Scanning == false {
							Scanning = true
							xbmc.VideoLibraryScan()
							Scanning = false
						}
					}
				}()
			}
		}()
	}

	log.Notice("Warming up caches...")
	go func() {
		time.Sleep(30 * time.Second)
		if !tmdb.WarmingUp.IsSet() {
			xbmc.Notify("Elementum", "LOCALIZE[30147]", config.AddonIcon())
		}
	}()

	started := time.Now()
	language := config.Get().Language
	tmdb.PopularMovies(tmdb.DiscoverFilters{}, language, 1)
	tmdb.PopularShows(tmdb.DiscoverFilters{}, language, 1)
	if _, _, err := trakt.TopMovies("trending", "1"); err != nil {
		log.Warning(err)
	}
	if _, _, err := trakt.TopShows("trending", "1"); err != nil {
		log.Warning(err)
	}

	tmdb.WarmingUp.Set()
	took := time.Since(started)
	if took.Seconds() > 30 {
		xbmc.Notify("Elementum", "LOCALIZE[30148]", config.AddonIcon())
	}
	log.Noticef("Caches warmed up in %s", took)

	updateFrequency := util.Max(1, config.Get().UpdateFrequency)
	traktFrequency := util.Max(1, config.Get().TraktSyncFrequency)

	updateTicker := time.NewTicker(time.Duration(updateFrequency) * time.Hour)
	traktSyncTicker := time.NewTicker(time.Duration(traktFrequency) * time.Hour)
	markedForRemovalTicker := time.NewTicker(30 * time.Second)

	defer updateTicker.Stop()
	defer traktSyncTicker.Stop()
	defer markedForRemovalTicker.Stop()

	closing := closer.C()

	for {
		select {
		case <-updateTicker.C:
			if config.Get().UpdateFrequency > 0 {
				go func() {
					if err := doUpdateLibrary(); err != nil {
						log.Warning(err)
					}
					if config.Get().UpdateAutoScan && Scanning == false && updateFrequency != traktFrequency {
						Scanning = true
						xbmc.VideoLibraryScan()
						Scanning = false
					}
				}()
			}
		case <-traktSyncTicker.C:
			if config.Get().TraktSyncFrequency > 0 && config.Get().TraktToken != "" {
				go func() {
					if err := RefreshTrakt(); err != nil {
						log.Warning(err)
					}
					if config.Get().UpdateAutoScan && Scanning == false {
						Scanning = true
						xbmc.VideoLibraryScan()
						Scanning = false
					}
				}()
			}
		case <-markedForRemovalTicker.C:
			rows, err := database.Get().Query(`SELECT infohash FROM tinfo WHERE state = 0`)
			if err != nil {
				log.Errorf("Cannot fetch existing torrents: %s", err)
				return
			}

			infoHash := ""
			for rows.Next() {
				rows.Scan(&infoHash)
				item := database.Get().GetBTItem(infoHash)

				if item == nil || item.State > 0 {
					continue
				}

				// Remove from Elementum's library to prevent duplicates
				if item.Type == movieType {
					if err := IsDuplicateMovie(strconv.Itoa(item.ID)); err != nil {
						if _, err := RemoveMovie(item.ID); err != nil {
							log.Warning("Nothing left to remove from Elementum")
						}
					}
				} else {
					if err := IsDuplicateEpisode(item.ShowID, item.Season, item.Episode); err != nil {
						if err := RemoveEpisode(item.ID, item.ShowID, item.Season, item.Episode); err != nil {
							log.Warning(err)
						}
					}
				}

				database.Get().DeleteBTItem(infoHash)
				log.Infof("Removed %s from database", infoHash)
			}
			rows.Close()
		case <-closing:
			return
		}
	}
}

// MoviesLibraryPath contains calculated path for saving Movies strm files
func MoviesLibraryPath() string {
	return filepath.Join(config.Get().LibraryPath, "Movies")
}

// ShowsLibraryPath contains calculated path for saving Shows strm files
func ShowsLibraryPath() string {
	return filepath.Join(config.Get().LibraryPath, "Shows")
}

//
// Library updates
//
func doUpdateLibrary() error {
	if err := checkShowsPath(); err != nil {
		return err
	}

	begin := time.Now()
	rows, err := database.Get().Query(`SELECT tmdbId, state, mediaType, showId FROM library_items WHERE mediaType = ? AND state = ?`, ShowType, StateActive)
	if err != nil {
		log.Errorf("Cannot fetch library shows: %s", err)
		return err
	}

	items := []*DBItem{}
	for rows.Next() {
		item := &DBItem{}
		rows.Scan(&item.ID, &item.State, &item.Type, &item.TVShowID)

		if item.ID == 0 {
			continue
		}
		items = append(items, item)
	}
	rows.Close()

	for _, item := range items {
		if _, err := writeShowStrm(item.ID, false, false); err != nil {
			log.Errorf("Error updating show: %s", err)
		}
	}

	log.Noticef("Library updated in %s", time.Since(begin))
	return nil
}

//
// Path checks
//
func checkLibraryPath() error {
	libraryPath := config.Get().LibraryPath
	if libraryPath == "" || libraryPath == "." {
		log.Warningf("Library path is not initialized")
		return errors.New("LOCALIZE[30220]")
	}
	if fileInfo, err := os.Stat(libraryPath); err != nil {
		if fileInfo == nil {
			log.Warningf("Library path is invalid")
			return errors.New("Invalid library path")
		}
		if !fileInfo.IsDir() {
			log.Warningf("Library path is not a directory")
			return errors.New("Library path is not a directory")
		}

		log.Warningf("Error getting Library path: %v", err)
		return err
	}
	return nil
}

func checkMoviesPath() error {
	if err := checkLibraryPath(); err != nil {
		return err
	}

	moviesLibraryPath := MoviesLibraryPath()
	if _, err := os.Stat(moviesLibraryPath); os.IsNotExist(err) {
		if err := os.Mkdir(moviesLibraryPath, 0755); err != nil {
			log.Error(err)
			return err
		}
	}
	return nil
}

func checkShowsPath() error {
	if err := checkLibraryPath(); err != nil {
		return err
	}

	showsLibraryPath := ShowsLibraryPath()
	if _, err := os.Stat(showsLibraryPath); os.IsNotExist(err) {
		if err := os.Mkdir(showsLibraryPath, 0755); err != nil {
			log.Error(err)
			return err
		}
	}
	return nil
}

//
// Writers
//

func writeMovieStrm(tmdbID string, force bool) (*tmdb.Movie, error) {
	movie := tmdb.GetMovieByID(tmdbID, config.Get().StrmLanguage)
	if movie == nil {
		return nil, errors.New("Can't find the movie")
	}

	movieName := movie.OriginalTitle
	if config.Get().StrmLanguage != config.Get().Language && movie.Title != "" {
		movieName = movie.Title
	}
	movieStrm := util.ToFileName(fmt.Sprintf("%s (%s)", movieName, strings.Split(movie.ReleaseDate, "-")[0]))
	moviePath := filepath.Join(MoviesLibraryPath(), movieStrm)

	if _, err := os.Stat(moviePath); os.IsNotExist(err) {
		if err := os.Mkdir(moviePath, 0755); err != nil {
			log.Error(err)
			return movie, err
		}
	} else if force {
		os.Chtimes(moviePath, time.Now().Local(), time.Now().Local())
	}

	movieStrmPath := filepath.Join(moviePath, fmt.Sprintf("%s.strm", movieStrm))
	if config.Get().LibraryNFOMovies {
		writeMovieNFO(movie, filepath.Join(moviePath, fmt.Sprintf("%s.nfo", movieStrm)))
	}

	playLink := URLForXBMC("/library/movie/play/%s", tmdbID)
	if _, err := os.Stat(movieStrmPath); !force && err == nil {
		// log.Debugf("Movie strm file already exists at %s", movieStrmPath)
		// return movie, fmt.Errorf("LOCALIZE[30287];;%s", movie.Title)
		return movie, nil
	}
	if err := ioutil.WriteFile(movieStrmPath, []byte(playLink), 0644); err != nil {
		log.Errorf("Could not write strm file: %s", err)
		return movie, err
	}

	return movie, nil
}

func writeMovieNFO(m *tmdb.Movie, p string) error {
	out := `<?xml version="1.0" encoding="UTF-8" standalone="yes" ?>
<movie>
	<uniqueid type="unknown" default="false">%v</uniqueid>
	<uniqueid type="elementum" default="false">%v</uniqueid>
	<uniqueid type="tmdb" default="true">%v</uniqueid>
	<uniqueid type="imdb" default="false">%v</uniqueid>
	<uniqueid type="tvdb" default="false">%v</uniqueid>
</movie>
https://www.themoviedb.org/movie/%v
`
	out = fmt.Sprintf(out,
		m.ID,
		m.ID,
		m.ID,
		m.ExternalIDs.IMDBId,
		m.ExternalIDs.TVDBID,
		m.ID,
	)

	if m.ExternalIDs.IMDBId != "" {
		out += fmt.Sprintf("https://www.imdb.com/title/%s/\n", m.ExternalIDs.IMDBId)
	}

	if err := ioutil.WriteFile(p, []byte(out), 0644); err != nil {
		log.Errorf("Could not write NFO file: %s", err)
		return err
	}

	return nil
}

func writeShowStrm(showID int, adding, force bool) (*tmdb.Show, error) {
	show := tmdb.GetShow(showID, config.Get().StrmLanguage)
	if show == nil {
		return nil, fmt.Errorf("Unable to get show (%d)", showID)
	}

	showName := show.OriginalName
	if config.Get().StrmLanguage != config.Get().Language && show.Name != "" {
		showName = show.Name
	}

	showStrm := util.ToFileName(fmt.Sprintf("%s (%s)", showName, strings.Split(show.FirstAirDate, "-")[0]))
	showPath := filepath.Join(ShowsLibraryPath(), showStrm)

	if _, err := os.Stat(showPath); os.IsNotExist(err) {
		if err := os.Mkdir(showPath, 0755); err != nil {
			log.Error(err)
			return show, err
		}
	} else if force {
		os.Chtimes(showPath, time.Now().Local(), time.Now().Local())
	}

	if config.Get().LibraryNFOShows {
		writeShowNFO(show, filepath.Join(showPath, "tvshow.nfo"))
	}

	now := util.UTCBod()
	addSpecials := config.Get().AddSpecials

	for i, season := range show.Seasons {
		if season.EpisodeCount == 0 {
			continue
		}
		if config.Get().ShowUnairedSeasons == false {
			firstAired, _ := time.Parse("2006-01-02", show.FirstAirDate)
			if firstAired.After(now) || firstAired.Equal(now) {
				continue
			}
		}
		if addSpecials == false && season.Season == 0 {
			continue
		}

		// Deleting last season from cache to always get the up-to-date data
		//  about last episodes
		if i == len(show.Seasons)-1 {
			cacheStore.Delete(fmt.Sprintf("com.tmdb.season.%d.%d.%s", showID, season.Season, config.Get().Language))
		}

		seasonTMDB := tmdb.GetSeason(showID, season.Season, config.Get().Language)
		if seasonTMDB == nil {
			continue
		}
		episodes := seasonTMDB.Episodes

		var reAddIDs []int
		for _, episode := range episodes {
			if episode == nil {
				continue
			}

			if config.Get().ShowUnairedEpisodes == false {
				if episode.AirDate == "" {
					continue
				}
				firstAired, _ := time.Parse("2006-01-02", episode.AirDate)
				if firstAired.After(now) || firstAired.Equal(now) {
					continue
				}
			}

			if adding {
				reAddIDs = append(reAddIDs, episode.ID)
			} else {
				// Check if single episode was previously removed
				if wasRemoved(episode.ID, EpisodeType) {
					continue
				}
			}

			if err := IsDuplicateEpisode(showID, season.Season, episode.EpisodeNumber); !force && err != nil {
				continue
			}

			episodeStrmPath := filepath.Join(showPath, fmt.Sprintf("%s S%02dE%02d.strm", showStrm, season.Season, episode.EpisodeNumber))
			playLink := URLForXBMC("/library/show/play/%d/%d/%d", showID, season.Season, episode.EpisodeNumber)
			if _, err := os.Stat(episodeStrmPath); !force && err == nil {
				continue
			}

			if err := ioutil.WriteFile(episodeStrmPath, []byte(playLink), 0644); err != nil {
				log.Error(err)
				return show, err
			}
		}
		if len(reAddIDs) > 0 {
			if err := updateBatchDBItem(reAddIDs, EpisodeType, StateActive, showID); err != nil {
				log.Error(err)
			}
		}
	}

	return show, nil
}

func writeShowNFO(s *tmdb.Show, p string) error {
	out := `<?xml version="1.0" encoding="UTF-8" standalone="yes" ?>
<tvshow>
	<uniqueid type="unknown" default="false">%v</uniqueid>
	<uniqueid type="elementum" default="false">%v</uniqueid>
	<uniqueid type="tmdb" default="true">%v</uniqueid>
	<uniqueid type="imdb" default="false">%v</uniqueid>
	<uniqueid type="tvdb" default="false">%v</uniqueid>
</tvshow>
https://www.themoviedb.org/tv/%v
`
	out = fmt.Sprintf(out,
		s.ID,
		s.ID,
		s.ID,
		s.ExternalIDs.IMDBId,
		s.ExternalIDs.TVDBID,
		s.ID,
	)

	if s.ExternalIDs.IMDBId != "" {
		out += fmt.Sprintf("https://www.imdb.com/title/%v/\n", s.ExternalIDs.IMDBId)
	}
	if s.ExternalIDs.TVDBID != "" {
		out += fmt.Sprintf("https://www.thetvdb.com/?tab=series&id=%v&lid=7\n", s.ExternalIDs.TVDBID)
	}

	if err := ioutil.WriteFile(p, []byte(out), 0644); err != nil {
		log.Errorf("Could not write NFO file: %s", err)
		return err
	}

	return nil
}

//
// Removers
//

// RemoveMovie removes movie from the library
func RemoveMovie(tmdbID int) (*tmdb.Movie, error) {
	if err := checkMoviesPath(); err != nil {
		return nil, err
	}
	defer func() {
		deleteDBItem(tmdbID, MovieType)
	}()

	ID := strconv.Itoa(tmdbID)
	movie := tmdb.GetMovieByID(ID, config.Get().StrmLanguage)
	if movie == nil {
		return nil, errors.New("Can't resolve movie")
	}

	titles := []string{movie.Title, movie.OriginalTitle}
	path := ""
	for _, t := range titles {
		movieStrm := util.ToFileName(fmt.Sprintf("%s (%s)", t, strings.Split(movie.ReleaseDate, "-")[0]))
		moviePath := filepath.Join(MoviesLibraryPath(), movieStrm)

		if _, err := os.Stat(moviePath); err == nil {
			path = moviePath
			break
		}
	}

	if path == "" {
		log.Warningf("Cannot stat movie strm file")
		return movie, errors.New("LOCALIZE[30282]")
	}
	if err := os.RemoveAll(path); err != nil {
		log.Warningf("Cannot remove movie strm file: %s", err)
		return movie, err
	}

	log.Warningf("%s removed from library", movie.Title)
	return movie, nil
}

// RemoveShow removes show from the library
func RemoveShow(tmdbID string) (*tmdb.Show, error) {
	if err := checkShowsPath(); err != nil {
		return nil, err
	}
	ID, _ := strconv.Atoi(tmdbID)
	defer func() {
		deleteDBItem(ID, ShowType)
	}()

	show := tmdb.GetShow(ID, config.Get().StrmLanguage)

	if show == nil {
		return nil, errors.New("Unable to find show to remove")
	}

	titles := []string{show.Name, show.OriginalName}
	path := ""
	for _, t := range titles {
		showStrm := util.ToFileName(fmt.Sprintf("%s (%s)", t, strings.Split(show.FirstAirDate, "-")[0]))
		showPath := filepath.Join(ShowsLibraryPath(), showStrm)

		if _, err := os.Stat(showPath); err == nil {
			path = showPath
			break
		}
	}

	if path == "" {
		log.Warningf("Cannot stat show strm file")
		return show, errors.New("LOCALIZE[30282]")
	}
	if err := os.RemoveAll(path); err != nil {
		log.Error(err)
		return show, err
	}

	log.Warningf("%s removed from library", show.Name)

	return show, nil
}

// RemoveEpisode removes episode from the library
func RemoveEpisode(tmdbID int, showID int, seasonNumber int, episodeNumber int) error {
	if err := checkShowsPath(); err != nil {
		return err
	}
	show := tmdb.GetShow(showID, config.Get().StrmLanguage)

	if show == nil {
		return errors.New("Unable to find show to remove episode")
	}

	showName := show.OriginalName
	if config.Get().StrmLanguage != config.Get().Language && show.Name != "" {
		showName = show.Name
	}

	showPath := util.ToFileName(fmt.Sprintf("%s (%s)", showName, strings.Split(show.FirstAirDate, "-")[0]))
	episodeStrm := fmt.Sprintf("%s S%02dE%02d.strm", showPath, seasonNumber, episodeNumber)
	episodePath := filepath.Join(ShowsLibraryPath(), showPath, episodeStrm)

	alreadyRemoved := false
	if _, err := os.Stat(episodePath); err != nil {
		alreadyRemoved = true
	}
	if !alreadyRemoved {
		if err := os.Remove(episodePath); err != nil {
			return err
		}
	}

	removedEpisodes <- &removedEpisode{
		ID:       tmdbID,
		ShowID:   showID,
		ShowName: show.Name,
		Season:   seasonNumber,
		Episode:  episodeNumber,
	}

	if !alreadyRemoved {
		log.Noticef("%s removed from library", episodeStrm)
	} else {
		return errors.New("Nothing left to remove from Elementum")
	}

	return nil
}

//
// Duplicate handling
//

// IsDuplicateMovie checks if movie exists in the library
func IsDuplicateMovie(tmdbID string) error {
	l.mu.UIDs.Lock()
	defer l.mu.UIDs.Unlock()

	query, _ := strconv.Atoi(tmdbID)
	for _, u := range l.UIDs {
		if u.TMDB != 0 && u.MediaType == MovieType && u.TMDB == query {
			return fmt.Errorf("%s already in library", tmdbID)
		}
	}

	return nil
}

// IsDuplicateShow checks if show exists in the library
func IsDuplicateShow(tmdbID string) error {
	show := tmdb.GetShowByID(tmdbID, config.Get().Language)
	if show == nil {
		return errors.New("Can't resolve show")
	}

	l.mu.UIDs.Lock()
	defer l.mu.UIDs.Unlock()

	query, _ := strconv.Atoi(tmdbID)
	for _, u := range l.UIDs {
		if u.TMDB != 0 && u.MediaType == ShowType && u.TMDB == query {
			return fmt.Errorf("%s already in library", show.Title)
		}
	}

	return nil
}

// IsDuplicateEpisode checks if episode exists in the library
func IsDuplicateEpisode(tmdbShowID int, seasonNumber int, episodeNumber int) (err error) {
	l.mu.Shows.Lock()
	defer l.mu.Shows.Unlock()

	for _, s := range l.Shows {
		if tmdbShowID != s.UIDs.TMDB {
			continue
		}

		for _, e := range s.Episodes {
			if e.Season == seasonNumber && e.Episode == episodeNumber {
				return fmt.Errorf("S%02dE%02d already in library", seasonNumber, episodeNumber)
			}
		}
	}

	return
}

// IsAddedToLibrary checks if specific TMDB exists in the library
func IsAddedToLibrary(id string, mediaType int) (isAdded bool) {
	count := 0
	database.Get().QueryRow("SELECT COUNT(*) FROM library_items WHERE tmdbId = ? AND mediaType = ? AND state = ?", id, mediaType, StateActive).Scan(&count)

	return count > 0
}

//
// Database updates
//

func updateDBItem(tmdbID int, state int, mediaType int, showID int) error {
	_, err := database.Get().Exec(`INSERT OR REPLACE INTO library_items (tmdbId, state, mediaType, showId) VALUES (?, ?, ?, ?)`, tmdbID, state, mediaType, showID)
	if err != nil {
		log.Debugf("updateDBItem failed: %s", err)
	}
	return err
}

func updateBatchDBItem(tmdbIds []int, state int, mediaType int, showID int) error {
	tx, err := database.Get().Begin()
	if err != nil {
		log.Debugf("Cannot start transaction: %s", err)
		return err
	}
	for _, id := range tmdbIds {
		_, err := tx.Exec(`INSERT OR REPLACE INTO library_items (tmdbId, state, mediaType, showId) VALUES (?, ?, ?, ?)`, id, state, mediaType, showID)
		if err != nil {
			log.Debugf("updateDBItem failed: %s", err)
			tx.Rollback()
			return err
		}
	}
	tx.Commit()

	return nil
}

func deleteDBItem(tmdbID int, mediaType int) error {
	_, err := database.Get().Exec(`UPDATE library_items SET state = ? WHERE tmdbId = ? AND mediaType = ?`, StateDeleted, tmdbID, mediaType)
	if err != nil {
		log.Debugf("deleteDBItem failed: %s", err)
	}
	return err
}

func deleteBatchDBItem(tmdbIds []int, mediaType int) error {
	tx, err := database.Get().Begin()
	if err != nil {
		log.Debugf("Cannot start transaction: %s", err)
		return err
	}
	for _, id := range tmdbIds {
		_, err := tx.Exec(`UPDATE library_items SET state = ? WHERE tmdbId = ? AND mediaType = ?`, StateDeleted, id, mediaType)
		if err != nil {
			log.Debugf("deleteDBItem failed: %s", err)
			tx.Rollback()
			return err
		}
	}
	tx.Commit()

	return nil
}

func wasRemoved(id int, mediaType int) (wasRemoved bool) {
	count := 0
	database.Get().QueryRow("SELECT COUNT(*) FROM library_items WHERE tmdbId = ? AND mediaType = ? AND state = ?", id, mediaType, StateDeleted).Scan(&count)

	return count > 0
}

//
// Maintenance
//

// CloseLibrary ...
func CloseLibrary() {
	log.Info("Closing library...")
	closer.Set()
}

// ClearPageCache deletes cached page listings
func ClearPageCache() {
	cacheDB := database.GetCache()
	if cacheDB != nil {
		cacheDB.DeleteWithPrefix(database.CommonBucket, []byte("page."))
	}
	xbmc.Refresh()
}

// ClearResolveCache deletes cached IDs resolve
func ClearResolveCache() {
	cacheDB := database.GetCache()
	if cacheDB != nil {
		cacheDB.DeleteWithPrefix(database.CommonBucket, []byte("Resolve_"))
	}
}

// ClearCacheKey deletes specific key
func ClearCacheKey(key string) {
	cacheDB := database.GetCache()
	if cacheDB != nil {
		log.Debugf("Removing cache key: %s", key)
		if err := cacheDB.Delete(database.CommonBucket, key); err != nil {
			log.Debugf("Error removing key from cache: %#v", err)
		}
	}
}

// ClearTraktCache deletes cached trakt data
func ClearTraktCache() {
	cacheDB := database.GetCache()
	if cacheDB != nil {
		cacheDB.DeleteWithPrefix(database.CommonBucket, []byte("com.trakt."))
	}
	xbmc.Refresh()
}

// ClearTmdbCache deletes cached tmdb data
func ClearTmdbCache() {
	cacheDB := database.GetCache()
	if cacheDB != nil {
		cacheDB.DeleteWithPrefix(database.CommonBucket, []byte("com.tmdb."))
	}
	xbmc.Refresh()
}

//
// Utilities
// 		mainly copied from api/routes to skip cycle imports

// URLForHTTP ...
func URLForHTTP(pattern string, args ...interface{}) string {
	u, _ := url.Parse(fmt.Sprintf(pattern, args...))
	return util.GetHTTPHost() + u.String()
}

// URLForXBMC ...
func URLForXBMC(pattern string, args ...interface{}) string {
	u, _ := url.Parse(fmt.Sprintf(pattern, args...))
	return "plugin://" + config.Get().Info.ID + u.String()
}

// URLQuery ...
func URLQuery(route string, query ...string) string {
	v := url.Values{}
	for i := 0; i < len(query); i += 2 {
		v.Add(query[i], query[i+1])
	}
	return route + "?" + v.Encode()
}

//
// Trakt syncs
//

// RefreshTrakt starts a trakt sync
func RefreshTrakt() error {
	if config.Get().TraktToken == "" {
		return nil
	}

	if Scanning {
		log.Debugf("TraktSync: already in scanning")
		return nil
	}

	Scanning = true
	defer func() {
		util.FreeMemoryGC()
		Scanning = false
	}()

	if err := checkMoviesPath(); err != nil {
		return err
	}
	if err := checkShowsPath(); err != nil {
		return err
	}

	log.Debugf("TraktSync: Watched")
	if changes, err := SyncTraktWatched(); err != nil {
		log.Debugf("TraktSync: Got error from SyncTraktWatched: %#v", err)
		// return err
	} else if changes {
		Refresh()
		xbmc.Refresh()
	}
	if config.Get().TraktSyncWatchlist {
		log.Debugf("TraktSync: Movies Watchlist")
		if err := SyncMoviesList("watchlist", true); err != nil {
			log.Debugf("TraktSync: Got error from SyncMoviesList: %#v", err)
			// return err
		}
		log.Debugf("TraktSync: Shows Watchlist")
		if err := SyncShowsList("watchlist", true); err != nil {
			log.Debugf("TraktSync: Got error from SyncShowsList: %#v", err)
			// return err
		}
	}
	if config.Get().TraktSyncCollections {
		log.Debugf("TraktSync: Movies Collections")
		if err := SyncMoviesList("collection", true); err != nil {
			log.Debugf("TraktSync: Got error from SyncMoviesList: %#v", err)
			// return err
		}
		log.Debugf("TraktSync: Shows Collections")
		if err := SyncShowsList("collection", true); err != nil {
			log.Debugf("TraktSync: Got error from SyncShowsList: %#v", err)
			// return err
		}
	}

	if config.Get().TraktSyncUserlists {
		log.Debugf("TraktSync: Userlists")
		lists := trakt.Userlists()
		for _, list := range lists {
			if err := SyncMoviesList(strconv.Itoa(list.IDs.Trakt), true); err != nil {
				continue
			}
			if err := SyncShowsList(strconv.Itoa(list.IDs.Trakt), true); err != nil {
				continue
			}
		}
	}

	log.Debugf("TraktSync: Finished")

	return nil
}

// SyncTraktWatched gets watched list and updates watched status in the library
func SyncTraktWatched() (haveChanges bool, err error) {
	if config.Get().TraktToken == "" || !config.Get().TraktSyncWatched {
		return
	}

	started := time.Now()
	TraktScanning = true
	defer func() {
		log.Debugf("Trakt sync watched finished in %s", time.Since(started))
		TraktScanning = false
		RefreshUIDs()
	}()

	previousMovies, _ := trakt.PreviousWatchedMovies()
	movies, errMovies := trakt.WatchedMovies()
	if errMovies != nil {
		return false, errMovies
	}

	diffMovies, _ := getUnwatchedMovies(movies, previousMovies)
	if len(diffMovies) > 0 {
		log.Infof("Unwatching %v movies", len(diffMovies))
	}

	for _, m := range diffMovies {
		var r *Movie
		if r == nil && m.Movie.IDs.TMDB != 0 {
			r, _ = GetMovieByTMDB(m.Movie.IDs.TMDB)
		}
		if r == nil && m.Movie.IDs.IMDB != "" {
			r, _ = GetMovieByIMDB(m.Movie.IDs.IMDB)
		}

		if r == nil {
			continue
		} else if r != nil && r.UIDs.Playcount > 0 {
			xbmc.SetMovieWatched(r.UIDs.Kodi, 0, 0, 0)
		}
	}

	l.mu.Trakt.Lock()

	l.WatchedTrakt = map[uint64]bool{}
	watchedMovies := map[int]bool{}
	for _, m := range movies {
		l.WatchedTrakt[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", MovieType, TraktScraper, m.Movie.IDs.Trakt))] = true
		l.WatchedTrakt[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", MovieType, TMDBScraper, m.Movie.IDs.TMDB))] = true
		l.WatchedTrakt[xxhash.Sum64String(fmt.Sprintf("%d_%d_%s", MovieType, IMDBScraper, m.Movie.IDs.IMDB))] = true

		var r *Movie
		if r == nil && m.Movie.IDs.TMDB != 0 {
			r, _ = GetMovieByTMDB(m.Movie.IDs.TMDB)
		}
		if r == nil && m.Movie.IDs.IMDB != "" {
			r, _ = GetMovieByIMDB(m.Movie.IDs.IMDB)
		}

		if r == nil {
			continue
		} else if r != nil {
			watchedMovies[r.UIDs.TMDB] = true

			if r.UIDs.Playcount == 0 {
				haveChanges = true
				xbmc.SetMovieWatchedWithDate(r.UIDs.Kodi, 1, int(r.Resume.Position), int(r.Resume.Total), m.LastWatchedAt)
			}
		}
	}
	l.mu.Trakt.Unlock()

	previousShows, _ := trakt.PreviousWatchedShows()
	shows, errShows := trakt.WatchedShows()
	if errShows != nil {
		return false, errShows
	}

	diffShows, _ := getUnwatchedShows(shows, previousShows)
	if len(diffShows) > 0 {
		log.Infof("Unwatching %v shows", len(diffShows))
	}

	for _, s := range diffShows {
		var r *Show
		if r == nil && s.Show.IDs.TMDB != 0 {
			r, _ = GetShowByTMDB(s.Show.IDs.TMDB)
		}
		if r == nil && s.Show.IDs.IMDB != "" {
			r, _ = GetShowByIMDB(s.Show.IDs.IMDB)
		}

		if r == nil {
			continue
		} else if r != nil {
			for _, season := range s.Seasons {
				for _, episode := range season.Episodes {
					if e := r.GetEpisode(season.Number, episode.Number); e != nil && e.UIDs.Playcount > 0 {
						xbmc.SetEpisodeWatched(e.UIDs.Kodi, 0, 0, 0)
					}
				}
			}
		}
	}

	l.mu.Trakt.Lock()

	watchedShows := map[int]bool{}
	for _, s := range shows {
		tmdbShow := tmdb.GetShowByID(strconv.Itoa(s.Show.IDs.TMDB), config.Get().Language)
		completedSeasons := 0
		for _, season := range s.Seasons {
			if tmdbShow != nil {
				tmdbSeason := tmdb.GetSeason(s.Show.IDs.TMDB, season.Number, config.Get().Language)
				if tmdbSeason != nil && tmdbSeason.EpisodeCount == len(season.Episodes) {
					completedSeasons++

					l.WatchedTrakt[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d", SeasonType, TMDBScraper, s.Show.IDs.TMDB, season.Number))] = true
					l.WatchedTrakt[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d", SeasonType, TraktScraper, s.Show.IDs.Trakt, season.Number))] = true
				}
			}

			for _, episode := range season.Episodes {
				l.WatchedTrakt[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d_%d", EpisodeType, TMDBScraper, s.Show.IDs.TMDB, season.Number, episode.Number))] = true
				l.WatchedTrakt[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d_%d", EpisodeType, TraktScraper, s.Show.IDs.Trakt, season.Number, episode.Number))] = true
			}
		}

		if tmdbShow != nil && completedSeasons == len(tmdbShow.Seasons) {
			s.Watched = true
			l.WatchedTrakt[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", ShowType, TMDBScraper, s.Show.IDs.TMDB))] = true
			l.WatchedTrakt[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", ShowType, TraktScraper, s.Show.IDs.Trakt))] = true
		}

		var r *Show
		if r == nil && s.Show.IDs.TMDB != 0 {
			r, _ = GetShowByTMDB(s.Show.IDs.TMDB)
		}
		if r == nil && s.Show.IDs.IMDB != "" {
			r, _ = GetShowByIMDB(s.Show.IDs.IMDB)
		}

		if r == nil {
			continue
		} else if r != nil {
			if s.Watched {
				watchedShows[r.UIDs.Kodi] = true
				xbmc.SetShowWatchedWithDate(r.UIDs.Kodi, 1, s.LastWatchedAt)
			}

			for _, season := range s.Seasons {
				for _, episode := range season.Episodes {
					e := r.GetEpisode(season.Number, episode.Number)
					if e != nil {
						watchedShows[e.UIDs.Kodi] = true

						if e.UIDs.Playcount == 0 {
							haveChanges = true
							xbmc.SetEpisodeWatchedWithDate(e.UIDs.Kodi, 1, int(e.Resume.Position), int(e.Resume.Total), episode.LastWatchedAt)
						}
					}
				}
			}
		}
	}
	l.mu.Trakt.Unlock()

	if !config.Get().TraktSyncWatchedBack {
		return
	}

	// Now, when we know what is marked Watched on Trakt - we are
	// looking at Kodi library and sync back to Trakt items,
	// watched in Kodi and not marked on Trakt
	syncMovies := []*trakt.WatchedItem{}
	syncShows := []*trakt.WatchedItem{}

	l.mu.Movies.Lock()
	for _, m := range l.Movies {
		if m.UIDs.TMDB == 0 {
			continue
		}
		cacheKey := fmt.Sprintf("Synced_%d_%d", MovieType, m.UIDs.TMDB)
		if _, ok := watchedMovies[m.UIDs.TMDB]; ok || m.UIDs.Playcount == 0 || database.GetCache().Has(database.CommonBucket, cacheKey) {
			continue
		}
		database.GetCache().Set(database.CommonBucket, cacheKey, "1")

		syncMovies = append(syncMovies, &trakt.WatchedItem{
			MediaType: "movie",
			Movie:     m.UIDs.TMDB,
			Watched:   true,
		})
	}
	l.mu.Movies.Unlock()

	l.mu.Shows.Lock()
	for _, s := range l.Shows {
		if s.UIDs.TMDB == 0 {
			continue
		}
		if _, ok := watchedShows[s.UIDs.Kodi]; ok {
			continue
		}

		for _, e := range s.Episodes {
			if _, ok := watchedShows[e.UIDs.Kodi]; ok || e.UIDs.Playcount == 0 {
				continue
			}

			cacheKey := fmt.Sprintf("Synced_%d_%d", EpisodeType, e.UIDs.Kodi)
			if database.GetCache().Has(database.CommonBucket, cacheKey) {
				continue
			}
			database.GetCache().Set(database.CommonBucket, cacheKey, "1")

			syncShows = append(syncShows, &trakt.WatchedItem{
				MediaType: "episode",
				Show:      s.UIDs.TMDB,
				Season:    e.Season,
				Episode:   e.Episode,
				Watched:   true,
			})
		}
	}
	l.mu.Shows.Unlock()

	if len(syncMovies) > 0 {
		trakt.SetMultipleWatched(syncMovies)
	}
	if len(syncShows) > 0 {
		trakt.SetMultipleWatched(syncShows)
	}

	return
}

//
// Movie internals
//

// SyncMoviesList updates trakt movie collections in cache
func SyncMoviesList(listID string, updating bool) (err error) {
	if err = checkMoviesPath(); err != nil {
		return
	}

	var label string
	var movies []*trakt.Movies

	switch listID {
	case "watchlist":
		movies, err = trakt.WatchlistMovies()
		label = "LOCALIZE[30254]"
	case "collection":
		movies, err = trakt.CollectionMovies()
		label = "LOCALIZE[30257]"
	default:
		movies, err = trakt.ListItemsMovies("", listID)
		label = "LOCALIZE[30263]"
	}

	if err != nil {
		log.Error(err)
		return
	}

	var movieIDs []int
	for _, movie := range movies {
		title := movie.Movie.Title
		// Try to resolve TMDB id through IMDB id, if provided
		if movie.Movie.IDs.TMDB == 0 && len(movie.Movie.IDs.IMDB) > 0 {
			r := tmdb.Find(movie.Movie.IDs.IMDB, "imdb_id")
			if r != nil && len(r.MovieResults) > 0 {
				movie.Movie.IDs.TMDB = r.MovieResults[0].ID
			}
		}

		if movie.Movie.IDs.TMDB == 0 {
			log.Warningf("Missing TMDB ID for %s", title)
			continue
		}

		tmdbID := strconv.Itoa(movie.Movie.IDs.TMDB)

		if updating && wasRemoved(movie.Movie.IDs.TMDB, MovieType) {
			continue
		}

		if err := IsDuplicateMovie(tmdbID); err != nil {
			continue
		}

		if _, err := writeMovieStrm(tmdbID, false); err != nil {
			continue
		}

		movieIDs = append(movieIDs, movie.Movie.IDs.TMDB)
	}

	if err := updateBatchDBItem(movieIDs, StateActive, MovieType, 0); err != nil {
		return err
	}

	if !updating {
		log.Noticef("Movies list (%s) added", listID)
		if config.Get().LibraryUpdate == 0 || (config.Get().LibraryUpdate == 1 && xbmc.DialogConfirmFocused("Elementum", fmt.Sprintf("LOCALIZE[30277];;%s", label))) {
			xbmc.VideoLibraryScan()
		}
	}
	return nil
}

//
// Shows internals
//

// SyncShowsList updates trakt collections in cache
func SyncShowsList(listID string, updating bool) (err error) {
	if err = checkShowsPath(); err != nil {
		return err
	}

	var label string
	var shows []*trakt.Shows

	switch listID {
	case "watchlist":
		shows, err = trakt.WatchlistShows()
		label = "LOCALIZE[30254]"
	case "collection":
		shows, err = trakt.CollectionShows()
		label = "LOCALIZE[30257]"
	default:
		shows, err = trakt.ListItemsShows(listID)
		label = "LOCALIZE[30263]"
	}

	if err != nil {
		log.Error(err)
		return
	}

	var showIDs []int
	for _, show := range shows {
		title := show.Show.Title
		// Try to resolve TMDB id through IMDB id, if provided
		if show.Show.IDs.TMDB == 0 {
			if len(show.Show.IDs.IMDB) > 0 {
				r := tmdb.Find(show.Show.IDs.IMDB, "imdb_id")
				if r != nil && len(r.TVResults) > 0 {
					show.Show.IDs.TMDB = r.TVResults[0].ID
				}
			}
			if show.Show.IDs.TMDB == 0 && show.Show.IDs.TVDB != 0 {
				r := tmdb.Find(strconv.Itoa(show.Show.IDs.TVDB), "tvdb_id")
				if r != nil && len(r.TVResults) > 0 {
					show.Show.IDs.TMDB = r.TVResults[0].ID
				}
			}
		}

		if show.Show.IDs.TMDB == 0 {
			log.Warningf("Missing TMDB ID for %s", title)
			continue
		}

		tmdbID := strconv.Itoa(show.Show.IDs.TMDB)

		if updating && wasRemoved(show.Show.IDs.TMDB, ShowType) {
			continue
		}

		if !updating {
			if err := IsDuplicateShow(tmdbID); err != nil {
				continue
			}
		}

		if _, err := writeShowStrm(show.Show.IDs.TMDB, false, false); err != nil {
			continue
		}

		showIDs = append(showIDs, show.Show.IDs.TMDB)
	}

	if err := updateBatchDBItem(showIDs, StateActive, ShowType, 0); err != nil {
		return err
	}

	if !updating {
		log.Noticef("Shows list (%s) added", listID)
		if config.Get().LibraryUpdate == 0 || (config.Get().LibraryUpdate == 1 && xbmc.DialogConfirmFocused("Elementum", fmt.Sprintf("LOCALIZE[30277];;%s", label))) {
			xbmc.VideoLibraryScan()
		}
	}
	return nil
}

//
// External handlers
//

// AddMovie is adding movie to the library
func AddMovie(tmdbID string, force bool) (*tmdb.Movie, error) {
	if err := checkMoviesPath(); err != nil {
		return nil, err
	}

	movie := tmdb.GetMovieByID(tmdbID, config.Get().Language)
	if movie == nil {
		return nil, fmt.Errorf("Movie with TMDB %s not found", tmdbID)
	}

	ID, _ := strconv.Atoi(tmdbID)
	if err := IsDuplicateMovie(tmdbID); !force && err != nil {
		log.Warningf("Error from duplicate movie: %s", err)
		xbmc.Notify("Elementum", fmt.Sprintf("LOCALIZE[30287];;%s", movie.Title), config.AddonIcon())
		return nil, err
	}

	if _, err := writeMovieStrm(tmdbID, force); err != nil {
		return movie, err
	}

	if err := updateDBItem(ID, StateActive, MovieType, 0); err != nil {
		return movie, err
	}

	log.Noticef("%s added to library", movie.Title)
	return movie, nil
}

// AddShow is adding show to the library
func AddShow(tmdbID string, force bool) (*tmdb.Show, error) {
	if err := checkShowsPath(); err != nil {
		return nil, err
	}

	ID, _ := strconv.Atoi(tmdbID)
	show := tmdb.GetShowByID(tmdbID, config.Get().Language)

	if err := IsDuplicateShow(tmdbID); !force && err != nil {
		log.Warning(err)
		xbmc.Notify("Elementum", fmt.Sprintf("LOCALIZE[30287];;%s", show.Name), config.AddonIcon())
		return show, err
	}

	if _, err := writeShowStrm(ID, true, force); err != nil {
		log.Error(err)
		return show, err
	}

	if err := updateDBItem(ID, StateActive, ShowType, 0); err != nil {
		return show, err
	}

	return show, nil
}

// GetMovieResume returns Resume info for kodi id
func GetMovieResume(kodiID int) *Resume {
	l.mu.Movies.Lock()
	defer l.mu.Movies.Unlock()

	for _, m := range l.Movies {
		if m.UIDs.Kodi == kodiID {
			return m.Resume
		}
	}

	return nil
}

// GetEpisodeResume returns Resume info for kodi id
func GetEpisodeResume(kodiID int) *Resume {
	l.mu.Shows.RLock()
	defer l.mu.Shows.RUnlock()

	for _, existingShow := range l.Shows {
		for _, existingEpisode := range existingShow.Episodes {
			if existingEpisode.UIDs.Kodi == kodiID {
				return existingEpisode.Resume
			}
		}
	}

	return nil
}

// GetUIDsFromKodi returns UIDs object for provided Kodi ID
func GetUIDsFromKodi(kodiID int) *UniqueIDs {
	if kodiID == 0 {
		return nil
	}

	l.mu.UIDs.Lock()
	defer l.mu.UIDs.Unlock()

	for _, u := range l.UIDs {
		if u.Kodi == kodiID {
			return u
		}
	}

	return nil
}

// GetShowForEpisode returns 'show' and 'episode'
func GetShowForEpisode(kodiID int) (*Show, *Episode) {
	if kodiID == 0 {
		return nil, nil
	}

	l.mu.Shows.Lock()
	defer l.mu.Shows.Unlock()

	for _, s := range l.Shows {
		for _, e := range s.Episodes {
			if e.UIDs.Kodi == kodiID {
				return s, e
			}
		}
	}

	return nil, nil
}

func getUnwatchedMovies(current, previous []*trakt.WatchedMovie) (diff []*trakt.WatchedMovie, err error) {
	if current == nil || previous == nil || len(previous) == 0 || len(current) == 0 {
		return
	}

	found := false
	for _, previousMovie := range previous {
		found = false
		for _, currentMovie := range current {
			if currentMovie.Movie.IDs.Trakt == previousMovie.Movie.IDs.Trakt {
				found = true
			}
		}

		if !found {
			diff = append(diff, previousMovie)
		}
	}

	return
}

func getUnwatchedShows(current, previous []*trakt.WatchedShow) (diff []*trakt.WatchedShow, err error) {
	if current == nil || previous == nil || len(previous) == 0 || len(current) == 0 {
		return
	}

	foundShow := false
	foundSeason := false
	foundEpisode := false

	var show *trakt.WatchedShow
	var season *trakt.WatchedSeason

	for _, previousShow := range previous {
		foundShow = false
		foundSeason = false
		foundEpisode = false

		show = nil

		for _, currentShow := range current {
			season = nil

			if previousShow.Show.IDs.Trakt == currentShow.Show.IDs.Trakt {
				foundShow = true

				for _, previousSeason := range previousShow.Seasons {
					foundSeason = false
					foundEpisode = false

					for _, currentSeason := range currentShow.Seasons {
						if previousSeason.Number == currentSeason.Number {
							foundSeason = true

							for _, previousEpisode := range previousSeason.Episodes {
								foundEpisode = false

								for _, currentEpisode := range currentSeason.Episodes {
									if previousEpisode.Number == currentEpisode.Number {
										foundEpisode = true
									}
								}

								if !foundEpisode {
									if season == nil {
										season = &trakt.WatchedSeason{Number: previousSeason.Number}
									}

									season.Episodes = append(season.Episodes, previousEpisode)
								}
							}
						}
					}

					if !foundSeason {
						season = previousSeason
					}
					if season != nil {
						if show == nil {
							show = &trakt.WatchedShow{Show: previousShow.Show}
						}

						show.Seasons = append(show.Seasons, season)
					}
				}
			}
		}

		if !foundShow {
			diff = append(diff, previousShow)
		}
		if show != nil {
			diff = append(diff, show)
		}
	}

	return
}
