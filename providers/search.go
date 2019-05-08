package providers

import (
	"io/ioutil"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/op/go-logging"
	"github.com/zeebo/bencode"

	"github.com/bcrusher29/solaris/bittorrent"
	"github.com/bcrusher29/solaris/config"
	"github.com/bcrusher29/solaris/tmdb"
	"github.com/bcrusher29/solaris/util"
	"github.com/bcrusher29/solaris/xbmc"
)

const (
	// SortMovies ...
	SortMovies = iota
	// SortShows ...
	SortShows
)

const (
	// SortBySeeders ...
	SortBySeeders = iota
	// SortByResolution ...
	SortByResolution
	// SortBalanced ...
	SortBalanced
	// SortBySize ...
	SortBySize
)

const (
	// Sort1080p720p480p ...
	Sort1080p720p480p = iota
	// Sort720p1080p480p ...
	Sort720p1080p480p
	// Sort720p480p1080p ...
	Sort720p480p1080p
	// Sort480p720p1080p ...
	Sort480p720p1080p
)

var (
	trackerTimeout = 6000 * time.Millisecond
	log            = logging.MustGetLogger("linkssearch")
)

// Search ...
func Search(searchers []Searcher, query string) []*bittorrent.TorrentFile {
	torrentsChan := make(chan *bittorrent.TorrentFile)
	go func() {
		wg := sync.WaitGroup{}
		for _, searcher := range searchers {
			wg.Add(1)
			go func(searcher Searcher) {
				defer wg.Done()
				for _, torrent := range searcher.SearchLinks(query) {
					torrentsChan <- torrent
				}
			}(searcher)
		}
		wg.Wait()
		close(torrentsChan)
	}()

	return processLinks(torrentsChan, SortMovies)
}

// SearchMovie ...
func SearchMovie(searchers []MovieSearcher, movie *tmdb.Movie) []*bittorrent.TorrentFile {
	torrentsChan := make(chan *bittorrent.TorrentFile)
	go func() {
		wg := sync.WaitGroup{}
		for _, searcher := range searchers {
			wg.Add(1)
			go func(searcher MovieSearcher) {
				defer wg.Done()
				for _, torrent := range searcher.SearchMovieLinks(movie) {
					torrentsChan <- torrent
				}
			}(searcher)
		}
		wg.Wait()
		close(torrentsChan)
	}()

	return processLinks(torrentsChan, SortMovies)
}

// SearchSeason ...
func SearchSeason(searchers []SeasonSearcher, show *tmdb.Show, season *tmdb.Season) []*bittorrent.TorrentFile {
	torrentsChan := make(chan *bittorrent.TorrentFile)
	go func() {
		wg := sync.WaitGroup{}
		for _, searcher := range searchers {
			wg.Add(1)
			go func(searcher SeasonSearcher) {
				defer wg.Done()
				for _, torrent := range searcher.SearchSeasonLinks(show, season) {
					torrentsChan <- torrent
				}
			}(searcher)
		}
		wg.Wait()
		close(torrentsChan)
	}()

	return processLinks(torrentsChan, SortShows)
}

// SearchEpisode ...
func SearchEpisode(searchers []EpisodeSearcher, show *tmdb.Show, episode *tmdb.Episode) []*bittorrent.TorrentFile {
	torrentsChan := make(chan *bittorrent.TorrentFile)
	go func() {
		wg := sync.WaitGroup{}
		for _, searcher := range searchers {
			wg.Add(1)
			go func(searcher EpisodeSearcher) {
				defer wg.Done()
				for _, torrent := range searcher.SearchEpisodeLinks(show, episode) {
					torrentsChan <- torrent
				}
			}(searcher)
		}
		wg.Wait()
		close(torrentsChan)
	}()

	return processLinks(torrentsChan, SortShows)
}

func processLinks(torrentsChan chan *bittorrent.TorrentFile, sortType int) []*bittorrent.TorrentFile {
	trackers := map[string]*bittorrent.Tracker{}
	torrentsMap := map[string]*bittorrent.TorrentFile{}

	torrents := make([]*bittorrent.TorrentFile, 0)

	log.Info("Resolving torrent files...")
	progress := 0
	progressTotal := 1
	progressUpdate := make(chan string)
	closed := util.Event{}

	defer func() {
		log.Debug("Closing progressupdate")
		closed.Set()
		close(progressUpdate)
	}()

	wg := sync.WaitGroup{}
	for torrent := range torrentsChan {
		wg.Add(1)
		if !strings.HasPrefix(torrent.URI, "magnet") {
			progressTotal++
		}
		torrents = append(torrents, torrent)
		go func(torrent *bittorrent.TorrentFile) {
			defer wg.Done()

			resolved := make(chan bool)
			failed := make(chan bool)

			go func(torrent *bittorrent.TorrentFile) {
				if err := torrent.Resolve(); err != nil {
					log.Warningf("Resolve failed for %s : %s", torrent.URI, err.Error())
					close(failed)
				}
				close(resolved)
			}(torrent)

			for {
				select {
				case <-time.After(trackerTimeout * 2): // Resolve timeout...
					return
				case <-failed:
					return
				case <-resolved:
					if closed.IsSet() {
						return
					}

					if !strings.HasPrefix(torrent.URI, "magnet") {
						progress++
						progressUpdate <- "LOCALIZE[30117]"
					} else {
						progressUpdate <- "skip"
					}

					return
				}
			}
		}(torrent)
	}

	dialogProgressBG := xbmc.NewDialogProgressBG("Elementum", "LOCALIZE[30117]", "LOCALIZE[30117]", "LOCALIZE[30118]")
	go func() {
		for {
			select {
			case <-time.After(trackerTimeout * 2):
				return
			case msg, ok := <-progressUpdate:
				if !ok {
					return
				}
				if dialogProgressBG != nil {
					if msg != "skip" {
						dialogProgressBG.Update(progress*100/progressTotal, "Elementum", msg)
					}
				} else {
					return
				}
			}
		}
	}()

	wg.Wait()

	dialogProgressBG.Update(100, "Elementum", "LOCALIZE[30117]")

	for _, torrent := range torrents {
		if torrent.InfoHash == "" {
			continue
		}

		torrentKey := torrent.InfoHash
		if torrent.IsPrivate {
			torrentKey = torrent.InfoHash + "-" + torrent.Provider
		}

		if existingTorrent, exists := torrentsMap[torrentKey]; exists {
			existingTorrent.Trackers = append(existingTorrent.Trackers, torrent.Trackers...)
			existingTorrent.Provider += ", " + torrent.Provider
			if torrent.Resolution > existingTorrent.Resolution {
				existingTorrent.Name = torrent.Name
				existingTorrent.Resolution = torrent.Resolution
			}
			if torrent.VideoCodec > existingTorrent.VideoCodec {
				existingTorrent.VideoCodec = torrent.VideoCodec
			}
			if torrent.AudioCodec > existingTorrent.AudioCodec {
				existingTorrent.AudioCodec = torrent.AudioCodec
			}
			if torrent.RipType > existingTorrent.RipType {
				existingTorrent.RipType = torrent.RipType
			}
			if torrent.SceneRating > existingTorrent.SceneRating {
				existingTorrent.SceneRating = torrent.SceneRating
			}
			if existingTorrent.Title == "" && torrent.Title != "" {
				existingTorrent.Title = torrent.Title
			}
			if existingTorrent.IsMagnet() && !torrent.IsMagnet() {
				existingTorrent.URI = torrent.URI
			}

			existingTorrent.Multi = true
		} else {
			torrentsMap[torrentKey] = torrent
		}

		for _, tracker := range torrent.Trackers {
			bTracker, err := bittorrent.NewTracker(tracker)
			if err != nil {
				continue
			}
			trackers[bTracker.URL.Host] = bTracker
		}

		if torrent.IsPrivate == false {
			for _, trackerURL := range bittorrent.DefaultTrackers {
				if tracker, err := bittorrent.NewTracker(trackerURL); err == nil && tracker != nil {
					trackers[tracker.URL.Host] = tracker
				}
			}
		}
	}

	torrents = make([]*bittorrent.TorrentFile, 0, len(torrentsMap))
	for _, torrent := range torrentsMap {
		torrents = append(torrents, torrent)
	}

	log.Infof("Received %d unique links.", len(torrents))

	if len(torrents) == 0 {
		dialogProgressBG.Close()
		return torrents
	}

	// log.Infof("Scraping torrent metrics from %d trackers...\n", len(trackers))

	// progressTotal = len(trackers)*2 + 1
	// progress = 0
	// progressMsg := "LOCALIZE[30118]"
	// dialogProgressBG.Update(progress*100/progressTotal, "Elementum", progressMsg)

	// scrapeResults := make(chan []bittorrent.ScrapeResponseEntry, len(trackers))
	// failedConnect := 0
	// failedScrape := 0
	// go func() {
	// 	wg := sync.WaitGroup{}
	// 	for _, tracker := range trackers {
	// 		wg.Add(1)
	// 		go func(tracker *bittorrent.Tracker) {
	// 			defer wg.Done()
	// 			defer func() {
	// 				progress += 2
	// 				if !closed.IsSet() {
	// 					progressUpdate <- progressMsg
	// 				}
	// 			}()

	// 			failed := make(chan bool)
	// 			connected := make(chan bool)
	// 			var scrapeResult []bittorrent.ScrapeResponseEntry

	// 			go func(tracker *bittorrent.Tracker) {
	// 				if err := tracker.Connect(); err != nil {
	// 					log.Warningf("Tracker %s failed: %s", tracker, err)
	// 					failedConnect++
	// 					close(failed)
	// 					return
	// 				}
	// 				close(connected)
	// 			}(tracker)

	// 			for {
	// 				select {
	// 				case <-failed:
	// 					return
	// 				case <-time.After(trackerTimeout): // Connect timeout...
	// 					failedConnect++
	// 					return
	// 				case <-connected:
	// 					scraped := make(chan bool)
	// 					go func(tracker *bittorrent.Tracker) {
	// 						scrapeResult = tracker.Scrape(torrents)
	// 						close(scraped)
	// 					}(tracker)

	// 					for {
	// 						select {
	// 						case <-time.After(trackerTimeout): // Scrape timeout...
	// 							failedScrape++
	// 							return
	// 						case <-scraped:
	// 							scrapeResults <- scrapeResult
	// 							return
	// 						}
	// 					}
	// 				}
	// 			}
	// 		}(tracker)
	// 	}
	// 	log.Debug("Waiting for scrape from trackers")
	// 	wg.Wait()

	// 	dialogProgressBG.Update(100, "Elementum", progressMsg)

	// 	if failedConnect > 0 {
	// 		log.Warningf("Failed to connect to %d tracker(s)", failedConnect)
	// 	}
	// 	if failedScrape > 0 {
	// 		log.Warningf("Failed to scrape results from %d tracker(s)", failedScrape)
	// 	} else if failedConnect > 0 {
	// 		log.Notice("Scraped all other trackers successfully")
	// 	} else {
	// 		log.Notice("Scraped all trackers successfully")
	// 	}

	// 	dialogProgressBG.Close()
	// 	dialogProgressBG = nil

	// 	close(scrapeResults)
	// }()

	// for results := range scrapeResults {
	// 	for i, result := range results {
	// 		if int64(result.Seeders) > torrents[i].Seeds {
	// 			torrents[i].Seeds = int64(result.Seeders)
	// 		}
	// 		if int64(result.Leechers) > torrents[i].Peers {
	// 			torrents[i].Peers = int64(result.Leechers)
	// 		}
	// 	}
	// }
	// log.Notice("Finished comparing seeds/peers of results to trackers...")

	dialogProgressBG.Close()
	dialogProgressBG = nil

	for _, t := range torrents {
		if _, err := os.Stat(t.URI); err != nil {
			continue
		}

		in, err := ioutil.ReadFile(t.URI)
		if err != nil {
			log.Debugf("Cannot read torrent file: %s", err)
			continue
		}

		var torrentFile *bittorrent.TorrentFileRaw
		err = bencode.DecodeBytes(in, &torrentFile)
		if err != nil {
			log.Debugf("Cannot decode torrent file: %s", err)
			continue
		}

		torrentFile.Title = t.Name
		torrentFile.Announce = ""
		torrentFile.AnnounceList = [][]string{}
		uniqueTrackers := map[string]struct{}{}
		for _, tr := range t.Trackers {
			if len(tr) == 0 {
				continue
			}

			uniqueTrackers[tr] = struct{}{}
		}
		for tr := range uniqueTrackers {
			torrentFile.AnnounceList = append(torrentFile.AnnounceList, []string{tr})
		}

		out, err := bencode.EncodeBytes(torrentFile)
		if err != nil {
			log.Debugf("Cannot encode torrent file: %s", err)
			continue
		}

		err = ioutil.WriteFile(t.URI, out, 0666)
		if err != nil {
			log.Debugf("Cannot write torrent file: %s", err)
			continue
		}

	}

	// Sorting resulting list of torrents
	conf := config.Get()
	sortMode := conf.SortingModeMovies
	resolutionPreference := conf.ResolutionPreferenceMovies

	if sortType == SortShows {
		sortMode = conf.SortingModeShows
		resolutionPreference = conf.ResolutionPreferenceShows
	}

	seeds := func(c1, c2 *bittorrent.TorrentFile) bool { return c1.Seeds > c2.Seeds }
	resolutionUp := func(c1, c2 *bittorrent.TorrentFile) bool { return c1.Resolution < c2.Resolution }
	resolutionDown := func(c1, c2 *bittorrent.TorrentFile) bool { return c1.Resolution > c2.Resolution }
	resolution720p1080p := func(c1, c2 *bittorrent.TorrentFile) bool { return Resolution720p1080p(c1) < Resolution720p1080p(c2) }
	resolution720p480p := func(c1, c2 *bittorrent.TorrentFile) bool { return Resolution720p480p(c1) < Resolution720p480p(c2) }
	balanced := func(c1, c2 *bittorrent.TorrentFile) bool { return float64(c1.Seeds) > Balanced(c2) }

	if sortMode == SortBySize {
		sort.Slice(torrents, func(i, j int) bool {
			return torrents[i].SizeParsed > torrents[j].SizeParsed
		})
	} else if sortMode == SortBySeeders {
		sort.Sort(sort.Reverse(BySeeds(torrents)))
	} else {
		switch resolutionPreference {
		case Sort1080p720p480p:
			if sortMode == SortBalanced {
				SortBy(balanced, resolutionDown).Sort(torrents)
			} else {
				SortBy(resolutionDown, seeds).Sort(torrents)
			}
			break
		case Sort480p720p1080p:
			if sortMode == SortBalanced {
				SortBy(balanced, resolutionUp).Sort(torrents)
			} else {
				SortBy(resolutionUp, seeds).Sort(torrents)
			}
			break
		case Sort720p1080p480p:
			if sortMode == SortBalanced {
				SortBy(balanced, resolution720p1080p).Sort(torrents)
			} else {
				SortBy(resolution720p1080p, seeds).Sort(torrents)
			}
			break
		case Sort720p480p1080p:
			if sortMode == SortBalanced {
				SortBy(balanced, resolution720p480p).Sort(torrents)
			} else {
				SortBy(resolution720p480p, seeds).Sort(torrents)
			}
			break
		}
	}

	// log.Info("Sorted torrent candidates.")
	// for _, torrent := range torrents {
	// 	log.Infof("S:%d P:%d %s - %s - %s", torrent.Seeds, torrent.Peers, torrent.Name, torrent.Provider, torrent.URI)
	// }

	return torrents
}
