package api

import (
	"encoding/json"
	"errors"
	"math/rand"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/bcrusher29/solaris/bittorrent"
	"github.com/bcrusher29/solaris/config"
	"github.com/bcrusher29/solaris/library"
	"github.com/bcrusher29/solaris/tmdb"
	"github.com/bcrusher29/solaris/xbmc"
	"github.com/sanity-io/litter"

	"github.com/gin-gonic/gin"
)

var (
	infoLabels = []string{
		"ListItem.DBID",
		"ListItem.DBTYPE",
		"ListItem.Mediatype",
		"ListItem.TMDB",
		"ListItem.UniqueId",

		"ListItem.Label",
		"ListItem.Label2",
		"ListItem.ThumbnailImage",
		"ListItem.Title",
		"ListItem.OriginalTitle",
		"ListItem.TVShowTitle",
		"ListItem.Season",
		"ListItem.Episode",
		"ListItem.Premiered",
		"ListItem.Plot",
		"ListItem.PlotOutline",
		"ListItem.Tagline",
		"ListItem.Year",
		"ListItem.Trailer",
		"ListItem.Studio",
		"ListItem.MPAA",
		"ListItem.Genre",
		"ListItem.Mediatype",
		"ListItem.Writer",
		"ListItem.Director",
		"ListItem.Rating",
		"ListItem.Votes",
		"ListItem.IMDBNumber",
		"ListItem.Code",
		"ListItem.ArtFanart",
		"ListItem.ArtBanner",
		"ListItem.ArtPoster",
		"ListItem.ArtTvshowPoster",
	}
)

func saveEncoded(encoded string) {
	xbmc.SetWindowProperty("ListItem.Encoded", encoded)
}

func encodeItem(item *xbmc.ListItem) string {
	data, _ := json.Marshal(item)

	return string(data)
}

// InfoLabelsStored ...
func InfoLabelsStored(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		labelsString := "{}"

		if listLabel := xbmc.InfoLabel("ListItem.Label"); len(listLabel) > 0 {
			labels := xbmc.InfoLabels(infoLabels...)

			listItemLabels := make(map[string]string, len(labels))
			for k, v := range labels {
				key := strings.Replace(k, "ListItem.", "", 1)
				listItemLabels[key] = v
			}

			b, _ := json.Marshal(listItemLabels)
			labelsString = string(b)
			saveEncoded(labelsString)
		} else if encoded := xbmc.GetWindowProperty("ListItem.Encoded"); len(encoded) > 0 {
			labelsString = encoded
		}

		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		ctx.String(200, labelsString)
	}
}

// InfoLabelsEpisode ...
func InfoLabelsEpisode(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		tmdbID := ctx.Params.ByName("showId")
		showID, _ := strconv.Atoi(tmdbID)
		seasonNumber, _ := strconv.Atoi(ctx.Params.ByName("season"))
		episodeNumber, _ := strconv.Atoi(ctx.Params.ByName("episode"))

		if item, err := GetEpisodeLabels(showID, seasonNumber, episodeNumber); err == nil {
			saveEncoded(encodeItem(item))
			ctx.JSON(200, item)
		} else {
			ctx.Error(err)
		}
	}
}

// InfoLabelsMovie ...
func InfoLabelsMovie(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		tmdbID := ctx.Params.ByName("tmdbId")

		if item, err := GetMovieLabels(tmdbID); err == nil {
			saveEncoded(encodeItem(item))
			ctx.JSON(200, item)
		} else {
			ctx.Error(err)
		}
	}
}

// InfoLabelsSearch ...
func InfoLabelsSearch(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		tmdbID := ctx.Params.ByName("tmdbId")

		if item, err := GetSearchLabels(s, tmdbID); err == nil {
			saveEncoded(encodeItem(item))
			ctx.JSON(200, item)
		} else {
			ctx.Error(err)
		}
	}
}

// GetEpisodeLabels returnes listitem for an episode
func GetEpisodeLabels(showID, seasonNumber, episodeNumber int) (item *xbmc.ListItem, err error) {
	show := tmdb.GetShow(showID, config.Get().Language)
	if show == nil {
		return nil, errors.New("Unable to find show")
	}

	season := tmdb.GetSeason(showID, seasonNumber, config.Get().Language)
	if season == nil {
		return nil, errors.New("Unable to find season")
	}

	episode := tmdb.GetEpisode(showID, seasonNumber, episodeNumber, config.Get().Language)
	if episode == nil {
		return nil, errors.New("Unable to find episode")
	}

	item = episode.ToListItem(show, season)
	if ls, err := library.GetShowByTMDB(show.ID); ls != nil && err == nil {
		log.Debugf("Found show in library: %s", litter.Sdump(ls.UIDs))
		if le := ls.GetEpisode(episode.SeasonNumber, episodeNumber); le != nil {
			item.Info.DBID = le.UIDs.Kodi
		}
	}
	if item.Art.FanArt == "" {
		fanarts := make([]string, 0)
		for _, backdrop := range show.Images.Backdrops {
			fanarts = append(fanarts, tmdb.ImageURL(backdrop.FilePath, "w1280"))
		}
		if len(fanarts) > 0 {
			item.Art.FanArt = fanarts[rand.Intn(len(fanarts))]
		}
	}
	item.Art.Poster = tmdb.ImageURL(season.Poster, "w500")

	return
}

// GetMovieLabels returnes listitem for a movie
func GetMovieLabels(tmdbID string) (item *xbmc.ListItem, err error) {
	movie := tmdb.GetMovieByID(tmdbID, config.Get().Language)
	if movie == nil {
		return nil, errors.New("Unable to find movie")
	}

	item = movie.ToListItem()
	if lm, err := library.GetMovieByTMDB(movie.ID); lm != nil && err == nil {
		log.Debugf("Found movie in library: %s", litter.Sdump(lm))
		item.Info.DBID = lm.UIDs.Kodi
	}

	return
}

// GetSearchLabels returnes listitem for a search query
func GetSearchLabels(s *bittorrent.Service, tmdbID string) (item *xbmc.ListItem, err error) {
	torrent := s.GetTorrentByFakeID(tmdbID)
	if torrent == nil || torrent.DBItem == nil {
		return nil, errors.New("Unable to find the torrent")
	}

	// Collecting downloaded file names into string to show in a subtitle
	chosenFiles := map[string]bool{}
	for _, f := range torrent.ChosenFiles {
		chosenFiles[filepath.Base(f.Path)] = true
	}
	chosenFileNames := []string{}
	for k := range chosenFiles {
		chosenFileNames = append(chosenFileNames, k)
	}
	sort.Sort(sort.StringSlice(chosenFileNames))
	subtitle := strings.Join(chosenFileNames, ", ")

	item = &xbmc.ListItem{
		Label:  torrent.DBItem.Query,
		Label2: subtitle,
		Info: &xbmc.ListItemInfo{
			Title:         torrent.DBItem.Query,
			OriginalTitle: torrent.DBItem.Query,
			TVShowTitle:   subtitle,
			DBTYPE:        "episode",
			Mediatype:     "episode",
		},
		Art: &xbmc.ListItemArt{},
	}

	return
}
