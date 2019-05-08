package trakt

import (
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bcrusher29/solaris/cache"
	"github.com/bcrusher29/solaris/config"
	"github.com/bcrusher29/solaris/playcount"
	"github.com/bcrusher29/solaris/tmdb"
	"github.com/bcrusher29/solaris/util"
	"github.com/bcrusher29/solaris/xbmc"
	"github.com/jmcvetta/napping"
)

// Fill fanart from TMDB
func setFanart(movie *Movie) *Movie {
	if movie.Images == nil {
		movie.Images = &Images{}
	}
	if movie.Images.Poster == nil {
		movie.Images.Poster = &Sizes{}
	}
	if movie.Images.Thumbnail == nil {
		movie.Images.Thumbnail = &Sizes{}
	}
	if movie.Images.FanArt == nil {
		movie.Images.FanArt = &Sizes{}
	}
	if movie.Images.Banner == nil {
		movie.Images.Banner = &Sizes{}
	}
	if movie.Images.ClearArt == nil {
		movie.Images.ClearArt = &Sizes{}
	}

	if movie.IDs == nil || movie.IDs.TMDB == 0 {
		return movie
	}

	tmdbImages := tmdb.GetImages(movie.IDs.TMDB)
	if tmdbImages == nil {
		return movie
	}

	if len(tmdbImages.Posters) > 0 {
		posterImage := tmdb.ImageURL(tmdbImages.Posters[0].FilePath, "w500")
		for _, image := range tmdbImages.Posters {
			if image.Iso639_1 == config.Get().Language {
				posterImage = tmdb.ImageURL(image.FilePath, "w500")
			}
		}
		movie.Images.Poster.Full = posterImage
		movie.Images.Thumbnail.Full = posterImage
	}
	if len(tmdbImages.Backdrops) > 0 {
		backdropImage := tmdb.ImageURL(tmdbImages.Backdrops[0].FilePath, "w1280")
		for _, image := range tmdbImages.Backdrops {
			if image.Iso639_1 == config.Get().Language {
				backdropImage = tmdb.ImageURL(image.FilePath, "w1280")
			}
		}
		movie.Images.FanArt.Full = backdropImage
		movie.Images.Banner.Full = backdropImage
	}
	return movie
}

func setFanarts(movies []*Movies) []*Movies {
	wg := sync.WaitGroup{}
	for i, movie := range movies {
		wg.Add(1)
		go func(idx int, m *Movies) {
			defer wg.Done()
			movies[idx].Movie = setFanart(m.Movie)
		}(i, movie)
	}
	wg.Wait()

	return movies
}

func setCalendarFanarts(movies []*CalendarMovie) []*CalendarMovie {
	wg := sync.WaitGroup{}
	for i, movie := range movies {
		wg.Add(1)
		go func(idx int, m *CalendarMovie) {
			defer wg.Done()
			movies[idx].Movie = setFanart(m.Movie)
		}(i, movie)
	}
	wg.Wait()

	return movies
}

// GetMovie ...
func GetMovie(ID string) (movie *Movie) {
	endPoint := fmt.Sprintf("movies/%s", ID)

	params := napping.Params{
		"extended": "full,images",
	}.AsUrlValues()

	cacheStore := cache.NewDBStore()
	key := fmt.Sprintf("com.trakt.movie.%s", ID)
	if err := cacheStore.Get(key, &movie); err != nil {
		resp, err := Get(endPoint, params)

		if err != nil {
			log.Error(err)
			xbmc.Notify("Elementum", fmt.Sprintf("Failed getting Trakt movie (%s), check your logs.", ID), config.AddonIcon())
		}

		if err := resp.Unmarshal(&movie); err != nil {
			log.Warning(err)
		}

		cacheStore.Set(key, movie, cacheExpiration)
	}

	return
}

// GetMovieByTMDB ...
func GetMovieByTMDB(tmdbID string) (movie *Movie) {
	endPoint := fmt.Sprintf("search/tmdb/%s?type=movie", tmdbID)

	params := napping.Params{}.AsUrlValues()

	cacheStore := cache.NewDBStore()
	key := fmt.Sprintf("com.trakt.movie.tmdb.%s", tmdbID)
	if err := cacheStore.Get(key, &movie); err != nil {
		resp, err := Get(endPoint, params)
		if err != nil {
			log.Error(err)
			xbmc.Notify("Elementum", "Failed getting Trakt movie using TMDB ID, check your logs.", config.AddonIcon())
			return
		}

		var results MovieSearchResults
		if err := resp.Unmarshal(&results); err != nil {
			log.Warning(err)
		}
		if results != nil && len(results) > 0 && results[0].Movie != nil {
			movie = results[0].Movie
		}
		cacheStore.Set(key, movie, cacheExpiration)
	}
	return
}

// SearchMovies ...
func SearchMovies(query string, page string) (movies []*Movies, err error) {
	endPoint := "search"

	params := napping.Params{
		"page":     page,
		"limit":    strconv.Itoa(config.Get().ResultsPerPage),
		"query":    query,
		"extended": "full,images",
	}.AsUrlValues()

	resp, err := Get(endPoint, params)

	if err != nil {
		return
	} else if resp.Status() != 200 {
		return movies, fmt.Errorf("Bad status searching Trakt movies: %d", resp.Status())
	}

	// TODO use response headers for pagination limits:
	// X-Pagination-Page-Count:10
	// X-Pagination-Item-Count:100

	if err := resp.Unmarshal(&movies); err != nil {
		log.Warning(err)
	}

	return
}

// TopMovies ...
func TopMovies(topCategory string, page string) (movies []*Movies, total int, err error) {
	endPoint := "movies/" + topCategory
	if topCategory == "recommendations" {
		endPoint = topCategory + "/movies"
	}

	resultsPerPage := config.Get().ResultsPerPage
	limit := resultsPerPage * PagesAtOnce
	pageInt, err := strconv.Atoi(page)
	if err != nil {
		return
	}
	page = strconv.Itoa((pageInt-1)*resultsPerPage/limit + 1)
	params := napping.Params{
		"page":     page,
		"limit":    strconv.Itoa(limit),
		"extended": "full,images",
	}.AsUrlValues()

	cacheStore := cache.NewDBStore()
	key := fmt.Sprintf("com.trakt.movies.%s.%s", topCategory, page)
	totalKey := fmt.Sprintf("com.trakt.movies.%s.total", topCategory)
	if err := cacheStore.Get(key, &movies); err != nil || len(movies) == 0 {
		var resp *napping.Response
		var err error

		if config.Get().TraktToken == "" {
			resp, err = Get(endPoint, params)
		} else {
			resp, err = GetWithAuth(endPoint, params)
		}

		if err != nil {
			return movies, 0, err
		} else if resp.Status() != 200 {
			return movies, 0, fmt.Errorf("Bad status getting top %s Trakt shows: %d", topCategory, resp.Status())
		}

		if topCategory == "popular" || topCategory == "recommendations" {
			var movieList []*Movie
			if errUnm := resp.Unmarshal(&movieList); errUnm != nil {
				log.Warning(errUnm)
			}

			movieListing := make([]*Movies, 0)
			for _, movie := range movieList {
				movieItem := Movies{
					Movie: movie,
				}
				movieListing = append(movieListing, &movieItem)
			}
			movies = movieListing
		} else {
			if errUnm := resp.Unmarshal(&movies); errUnm != nil {
				log.Warning(errUnm)
			}
		}

		pagination := getPagination(resp.HttpResponse().Header)
		total = pagination.ItemCount
		if err != nil {
			log.Warning(err)
		} else {
			cacheStore.Set(totalKey, total, recentExpiration)
		}

		cacheStore.Set(key, movies, recentExpiration)
	} else {
		if err := cacheStore.Get(totalKey, &total); err != nil {
			total = -1
		}
	}

	return
}

// WatchlistMovies ...
func WatchlistMovies() (movies []*Movies, err error) {
	if err := Authorized(); err != nil {
		return movies, err
	}

	endPoint := "sync/watchlist/movies"

	params := napping.Params{
		"extended": "full,images",
	}.AsUrlValues()

	cacheStore := cache.NewDBStore()
	key := "com.trakt.movies.watchlist"
	if err := cacheStore.Get(key, &movies); err != nil {
		resp, err := GetWithAuth(endPoint, params)

		if err != nil {
			return movies, err
		} else if resp.Status() != 200 {
			return movies, fmt.Errorf("Bad status getting Trakt watchlist for movies: %d", resp.Status())
		}

		var watchlist []*WatchlistMovie
		if err := resp.Unmarshal(&watchlist); err != nil {
			log.Warning(err)
		}

		movieListing := make([]*Movies, 0)
		for _, movie := range watchlist {
			movieItem := Movies{
				Movie: movie.Movie,
			}
			movieListing = append(movieListing, &movieItem)
		}
		movies = movieListing

		cacheStore.Set(key, movies, userlistExpiration)
	}

	return
}

// CollectionMovies ...
func CollectionMovies() (movies []*Movies, err error) {
	if errAuth := Authorized(); errAuth != nil {
		return movies, errAuth
	}

	endPoint := "sync/collection/movies"

	params := napping.Params{
		"extended": "full,images",
	}.AsUrlValues()

	cacheStore := cache.NewDBStore()
	key := "com.trakt.movies.collection"
	if errGet := cacheStore.Get(key, &movies); errGet != nil {
		resp, errGet := GetWithAuth(endPoint, params)

		if errGet != nil {
			return movies, errGet
		} else if resp.Status() != 200 {
			return movies, fmt.Errorf("Bad status getting Trakt collection for movies: %d", resp.Status())
		}

		var collection []*CollectionMovie
		resp.Unmarshal(&collection)

		movieListing := make([]*Movies, 0)
		for _, movie := range collection {
			movieItem := Movies{
				Movie: movie.Movie,
			}
			movieListing = append(movieListing, &movieItem)
		}
		movies = movieListing

		cacheStore.Set(key, movies, userlistExpiration)
	}

	return movies, err
}

// Userlists ...
func Userlists() (lists []*List) {
	traktUsername := config.Get().TraktUsername
	if traktUsername == "" {
		xbmc.Notify("Elementum", "LOCALIZE[30149]", config.AddonIcon())
		return lists
	}
	endPoint := fmt.Sprintf("users/%s/lists", traktUsername)

	params := napping.Params{}.AsUrlValues()

	var resp *napping.Response
	var err error

	if config.Get().TraktToken == "" {
		resp, err = Get(endPoint, params)
	} else {
		resp, err = GetWithAuth(endPoint, params)
	}

	if err != nil {
		xbmc.Notify("Elementum", err.Error(), config.AddonIcon())
		log.Error(err)
		return lists
	}
	if resp.Status() != 200 {
		errMsg := fmt.Sprintf("Bad status getting custom lists for %s: %d", traktUsername, resp.Status())
		xbmc.Notify("Elementum", errMsg, config.AddonIcon())
		log.Warningf(errMsg)
		return lists
	}

	if err := resp.Unmarshal(&lists); err != nil {
		log.Warning(err)
	}

	sort.Slice(lists, func(i int, j int) bool {
		return lists[i].Name < lists[j].Name
	})

	return lists
}

// Likedlists ...
func Likedlists() (lists []*List) {
	traktUsername := config.Get().TraktUsername
	if traktUsername == "" {
		xbmc.Notify("Elementum", "LOCALIZE[30149]", config.AddonIcon())
		return lists
	}
	endPoint := "users/likes"
	params := napping.Params{}.AsUrlValues()

	var resp *napping.Response
	var err error

	if config.Get().TraktToken == "" {
		resp, err = Get(endPoint, params)
	} else {
		resp, err = GetWithAuth(endPoint, params)
	}

	if err != nil {
		xbmc.Notify("Elementum", err.Error(), config.AddonIcon())
		log.Error(err)
		return lists
	}
	if resp.Status() != 200 {
		errMsg := fmt.Sprintf("Bad status getting liked lists for %s: %d", traktUsername, resp.Status())
		xbmc.Notify("Elementum", errMsg, config.AddonIcon())
		log.Warningf(errMsg)
		return lists
	}

	inputLists := []*ListContainer{}
	if err := resp.Unmarshal(&inputLists); err != nil {
		log.Warning(err)
	}

	for _, l := range inputLists {
		lists = append(lists, l.List)
	}

	sort.Slice(lists, func(i int, j int) bool {
		return lists[i].Name < lists[j].Name
	})

	return lists
}

// TopLists ...
func TopLists(page string) (lists []*ListContainer, hasNext bool) {
	pageInt, _ := strconv.Atoi(page)

	endPoint := "lists/popular"
	params := napping.Params{
		"page":  page,
		"limit": strconv.Itoa(config.Get().ResultsPerPage),
	}.AsUrlValues()

	var resp *napping.Response
	var err error

	if config.Get().TraktToken == "" {
		resp, err = Get(endPoint, params)
	} else {
		resp, err = GetWithAuth(endPoint, params)
	}

	if err != nil {
		xbmc.Notify("Elementum", err.Error(), config.AddonIcon())
		log.Error(err)
		return lists, hasNext
	}
	if resp.Status() != 200 {
		errMsg := fmt.Sprintf("Bad status getting top lists: %d", resp.Status())
		xbmc.Notify("Elementum", errMsg, config.AddonIcon())
		log.Warningf(errMsg)
		return lists, hasNext
	}

	if err := resp.Unmarshal(&lists); err != nil {
		log.Warning(err)
	}

	sort.Slice(lists, func(i int, j int) bool {
		return lists[i].List.Name < lists[j].List.Name
	})

	p := getPagination(resp.HttpResponse().Header)
	hasNext = p.PageCount > pageInt

	return lists, hasNext
}

// ListItemsMovies ...
func ListItemsMovies(user string, listID string) (movies []*Movies, err error) {
	if user == "" || user == "id" {
		user = config.Get().TraktUsername
	}

	endPoint := fmt.Sprintf("users/%s/lists/%s/items/movies", user, listID)

	params := napping.Params{}.AsUrlValues()

	var resp *napping.Response

	cacheStore := cache.NewDBStore()
	key := fmt.Sprintf("com.trakt.movies.list.%s", listID)
	if errGet := cacheStore.Get(key, &movies); errGet != nil {
		if config.Get().TraktToken == "" {
			resp, errGet = Get(endPoint, params)
		} else {
			resp, errGet = GetWithAuth(endPoint, params)
		}

		if errGet != nil || resp.Status() != 200 {
			return movies, errGet
		}

		var list []*ListItem
		if err = resp.Unmarshal(&list); err != nil {
			log.Warning(err)
		}

		movieListing := make([]*Movies, 0)
		for _, movie := range list {
			if movie.Movie == nil {
				continue
			}
			movieItem := Movies{
				Movie: movie.Movie,
			}
			movieListing = append(movieListing, &movieItem)
		}
		movies = movieListing

		cacheStore.Set(key, movies, 1*time.Minute)
	}

	return movies, err
}

// CalendarMovies ...
func CalendarMovies(endPoint string, page string) (movies []*CalendarMovie, total int, err error) {
	resultsPerPage := config.Get().ResultsPerPage
	limit := resultsPerPage * PagesAtOnce
	pageInt, err := strconv.Atoi(page)
	if err != nil {
		return
	}
	page = strconv.Itoa((pageInt-1)*resultsPerPage/limit + 1)
	params := napping.Params{
		"page":     page,
		"limit":    strconv.Itoa(limit),
		"extended": "full,images",
	}.AsUrlValues()

	cacheStore := cache.NewDBStore()
	endPointKey := strings.Replace(endPoint, "/", ".", -1)
	key := fmt.Sprintf("com.trakt.mymovies.%s.%s", endPointKey, page)
	totalKey := fmt.Sprintf("com.trakt.mymovies.%s.total", endPointKey)
	if err := cacheStore.Get(key, &movies); err != nil {
		resp, err := GetWithAuth("calendars/"+endPoint, params)

		if err != nil {
			log.Error(err)
			return movies, 0, err
		} else if resp.Status() != 200 {
			log.Warning(resp.Status())
			return movies, 0, fmt.Errorf("Bad status getting %s Trakt movies: %d", endPoint, resp.Status())
		}

		if errUnm := resp.Unmarshal(&movies); errUnm != nil {
			log.Warning(errUnm)
		}

		pagination := getPagination(resp.HttpResponse().Header)
		total = pagination.ItemCount
		if err != nil {
			total = -1
		} else {
			cacheStore.Set(totalKey, total, recentExpiration)
		}

		cacheStore.Set(key, movies, recentExpiration)
	} else {
		if err := cacheStore.Get(totalKey, &total); err != nil {
			total = -1
		}
	}

	return
}

// WatchedMovies ...
func WatchedMovies() (movies []*WatchedMovie, err error) {
	if err := Authorized(); err != nil {
		return movies, nil
	}

	lastActivities, errAct := GetLastActivities()
	if errAct != nil {
		return movies, errAct
	}

	cacheStore := cache.NewDBStore()
	key := "com.trakt.movies.watched"
	keyLong := "com.trakt.movies.watched.previous"
	watchedKey := "com.trakt.progress.movies.watched"

	defer cacheStore.Set(watchedKey, lastActivities.Episodes.WatchedAt, activitiesExpiration)

	var cachedWatchedAt time.Time
	cacheStore.Get(watchedKey, &cachedWatchedAt)
	if err := cacheStore.Get(watchedKey, &cachedWatchedAt); err == nil && !lastActivities.Movies.WatchedAt.After(cachedWatchedAt) {
		if err := cacheStore.Get(key, &movies); err == nil {
			return movies, nil
		}
	}

	endPoint := "sync/watched/movies"
	params := napping.Params{}.AsUrlValues()

	resp, err := GetWithAuth(endPoint, params)

	if err != nil {
		return movies, err
	} else if resp.Status() != 200 {
		return movies, fmt.Errorf("Bad status getting Trakt watched for movies: %d", resp.Status())
	}

	if err := resp.Unmarshal(&movies); err != nil {
		log.Warning(err)
	}

	sort.Slice(movies, func(i int, j int) bool {
		return movies[i].LastWatchedAt.Unix() > movies[j].LastWatchedAt.Unix()
	})

	cacheStore.Set(key, movies, progressExpiration)
	cacheStore.Set(keyLong, movies, watchedLongExpiration)

	return
}

// PreviousWatchedMovies ...
func PreviousWatchedMovies() (movies []*WatchedMovie, err error) {
	cacheStore := cache.NewDBStore()
	keyLong := "com.trakt.movies.watched.previous"
	err = cacheStore.Get(keyLong, &movies)
	return
}

// ToListItem ...
func (movie *Movie) ToListItem() (item *xbmc.ListItem) {
	if !config.Get().ForceUseTrakt && movie.IDs.TMDB != 0 {
		tmdbID := strconv.Itoa(movie.IDs.TMDB)
		if tmdbMovie := tmdb.GetMovieByID(tmdbID, config.Get().Language); tmdbMovie != nil {
			item = tmdbMovie.ToListItem()
		}
	}
	if item == nil {
		movie = setFanart(movie)
		item = &xbmc.ListItem{
			Label: movie.Title,
			Info: &xbmc.ListItemInfo{
				Count:         rand.Int(),
				Title:         movie.Title,
				OriginalTitle: movie.Title,
				Year:          movie.Year,
				Genre:         strings.Title(strings.Join(movie.Genres, " / ")),
				Plot:          movie.Overview,
				PlotOutline:   movie.Overview,
				TagLine:       movie.TagLine,
				Rating:        movie.Rating,
				Votes:         strconv.Itoa(movie.Votes),
				Duration:      movie.Runtime * 60,
				MPAA:          movie.Certification,
				Code:          movie.IDs.IMDB,
				IMDBNumber:    movie.IDs.IMDB,
				Trailer:       util.TrailerURL(movie.Trailer),
				PlayCount:     playcount.GetWatchedMovieByTMDB(movie.IDs.TMDB).Int(),
				DBTYPE:        "movie",
				Mediatype:     "movie",
			},
			Art: &xbmc.ListItemArt{
				Poster:    movie.Images.Poster.Full,
				FanArt:    movie.Images.FanArt.Full,
				Banner:    movie.Images.Banner.Full,
				Thumbnail: movie.Images.Thumbnail.Full,
				ClearArt:  movie.Images.ClearArt.Full,
			},
			Thumbnail: movie.Images.Poster.Full,
		}
	}

	if len(item.Info.Trailer) == 0 {
		item.Info.Trailer = util.TrailerURL(movie.Trailer)
	}

	return
}
