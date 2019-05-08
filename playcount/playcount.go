package playcount

import (
	"fmt"
	"sync"

	"github.com/cespare/xxhash"
	"github.com/op/go-logging"
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
	// MovieType ...
	MovieType = iota
	// ShowType ...
	ShowType
	// SeasonType ...
	SeasonType
	// EpisodeType ...
	EpisodeType
)

// Watched stores all "watched" items
var (
	// Mu is a global lock for Playcount package
	Mu = sync.RWMutex{}

	// Watched contains uint64 hashed bools
	Watched = map[uint64]bool{}

	log = logging.MustGetLogger("playcount")
)

// WatchedState just a simple bool with Int() conversion
type WatchedState bool

// GetWatchedMovieByTMDB checks whether item is watched
func GetWatchedMovieByTMDB(id int) (ret WatchedState) {
	Mu.RLock()
	defer Mu.RUnlock()

	_, ret = Watched[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", MovieType, TMDBScraper, id))]
	return
}

// GetWatchedMovieByIMDB checks whether item is watched
func GetWatchedMovieByIMDB(id string) (ret WatchedState) {
	Mu.RLock()
	defer Mu.RUnlock()

	_, ret = Watched[xxhash.Sum64String(fmt.Sprintf("%d_%d_%s", MovieType, IMDBScraper, id))]
	return
}

// GetWatchedMovieByTrakt checks whether item is watched
func GetWatchedMovieByTrakt(id int) (ret WatchedState) {
	Mu.RLock()
	defer Mu.RUnlock()

	_, ret = Watched[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", MovieType, TraktScraper, id))]
	return
}

// GetWatchedShowByTMDB checks whether item is watched
func GetWatchedShowByTMDB(id int) (ret WatchedState) {
	Mu.RLock()
	defer Mu.RUnlock()

	_, ret = Watched[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", ShowType, TMDBScraper, id))]
	return
}

// GetWatchedShowByTVDB checks whether item is watched
func GetWatchedShowByTVDB(id int) (ret WatchedState) {
	Mu.RLock()
	defer Mu.RUnlock()

	_, ret = Watched[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", ShowType, TVDBScraper, id))]
	return
}

// GetWatchedShowByTrakt checks whether item is watched
func GetWatchedShowByTrakt(id int) (ret WatchedState) {
	Mu.RLock()
	defer Mu.RUnlock()

	_, ret = Watched[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", ShowType, TraktScraper, id))]
	return
}

// GetWatchedSeasonByTMDB checks whether item is watched
func GetWatchedSeasonByTMDB(id int, season int) (ret WatchedState) {
	Mu.RLock()
	defer Mu.RUnlock()

	_, ret = Watched[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d", SeasonType, TMDBScraper, id, season))]
	return
}

// GetWatchedSeasonByTVDB checks whether item is watched
func GetWatchedSeasonByTVDB(id int, season, episode int) (ret WatchedState) {
	Mu.RLock()
	defer Mu.RUnlock()

	_, ret = Watched[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d", SeasonType, TVDBScraper, id, season))]
	return
}

// GetWatchedSeasonByTrakt checks whether item is watched
func GetWatchedSeasonByTrakt(id int, season int) (ret WatchedState) {
	Mu.RLock()
	defer Mu.RUnlock()

	_, ret = Watched[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d", SeasonType, TraktScraper, id, season))]
	return
}

// GetWatchedEpisodeByTMDB checks whether item is watched
func GetWatchedEpisodeByTMDB(id int, season, episode int) (ret WatchedState) {
	Mu.RLock()
	defer Mu.RUnlock()

	_, ret = Watched[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d_%d", EpisodeType, TMDBScraper, id, season, episode))]
	return
}

// GetWatchedEpisodeByTVDB checks whether item is watched
func GetWatchedEpisodeByTVDB(id int, season, episode int) (ret WatchedState) {
	Mu.RLock()
	defer Mu.RUnlock()

	_, ret = Watched[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d_%d", EpisodeType, TVDBScraper, id, season, episode))]
	return
}

// GetWatchedEpisodeByTrakt checks whether item is watched
func GetWatchedEpisodeByTrakt(id int, season, episode int) (ret WatchedState) {
	Mu.RLock()
	defer Mu.RUnlock()

	_, ret = Watched[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d_%d", EpisodeType, TraktScraper, id, season, episode))]
	return
}

// Int converts bool to int
func (w WatchedState) Int() (r int) {
	if w {
		r = 1
	}

	return
}
