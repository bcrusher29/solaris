package api

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/bcrusher29/solaris/bittorrent"
	"github.com/bcrusher29/solaris/database"
	"github.com/bcrusher29/solaris/util"
	"github.com/bcrusher29/solaris/xbmc"

	"github.com/gin-gonic/gin"
	"github.com/sanity-io/litter"
)

// Play ...
func Play(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		uri := ctx.Query("uri")
		index := ctx.Query("index")
		resume := ctx.Query("resume")
		doresume := ctx.DefaultQuery("doresume", "true")
		query := ctx.Query("query")
		contentType := ctx.Query("type")
		tmdb := ctx.Query("tmdb")
		show := ctx.Query("show")
		season := ctx.Query("season")
		episode := ctx.Query("episode")

		if uri == "" && resume == "" {
			return
		}

		fileIndex := -1
		if index != "" {
			if position, err := strconv.Atoi(index); err == nil && position >= 0 {
				fileIndex = position
			}
		}

		tmdbID := 0
		if tmdb != "" {
			if id, err := strconv.Atoi(tmdb); err == nil && id > 0 {
				tmdbID = id
			}
		}

		showID := 0
		if show != "" {
			if id, err := strconv.Atoi(show); err == nil && id > 0 {
				showID = id
			}
		}

		seasonNumber := 0
		if season != "" {
			if number, err := strconv.Atoi(season); err == nil && number > 0 {
				seasonNumber = number
			}
		}

		episodeNumber := 0
		if episode != "" {
			if number, err := strconv.Atoi(episode); err == nil && number > 0 {
				episodeNumber = number
			}
		}

		params := bittorrent.PlayerParams{
			URI:            uri,
			FileIndex:      fileIndex,
			ResumeHash:     resume,
			ResumePlayback: doresume != "false",
			KodiPosition:   -1,
			ContentType:    contentType,
			TMDBId:         tmdbID,
			ShowID:         showID,
			Season:         seasonNumber,
			Episode:        episodeNumber,
			Query:          query,
		}

		player := bittorrent.NewPlayer(s, params)
		log.Debugf("Playing item: %s", litter.Sdump(params))
		if t := s.GetTorrentByHash(resume); resume != "" && t != nil {
			player.SetTorrent(t)
		}
		if player.Buffer() != nil || !player.HasChosenFile() {
			player.Close()
			return
		}

		rURL, _ := url.Parse(fmt.Sprintf("%s/files/%s", util.GetContextHTTPHost(ctx), player.PlayURL()))
		ctx.Redirect(302, rURL.String())
	}
}

// PlayTorrent ...
func PlayTorrent(ctx *gin.Context) {
	retval := xbmc.DialogInsert()
	if retval["path"] == "" {
		return
	}
	xbmc.PlayURLWithTimeout(URLQuery(URLForXBMC("/play"), "uri", retval["path"]))

	ctx.String(200, "")
	return
}

// PlayURI ...
func PlayURI(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		uri := ctx.Request.FormValue("uri")
		file, header, fileError := ctx.Request.FormFile("file")

		if file != nil && header != nil && fileError == nil {
			t, err := saveTorrentFile(file, header)
			if err == nil && t != "" {
				uri = t
			}
		}

		index := ctx.Query("index")
		resume := ctx.Query("resume")

		if uri == "" && resume == "" {
			return
		}

		if uri != "" {
			xbmc.PlayURL(URLQuery(URLForXBMC("/play"), "uri", uri, "index", index))
		} else {
			var (
				tmdb        string
				show        string
				season      string
				episode     string
				query       string
				contentType string
			)
			t := s.GetTorrentByHash(resume)

			if t != nil {
				infoHash := t.InfoHash()
				dbItem := database.Get().GetBTItem(infoHash)
				if dbItem != nil && dbItem.Type != "" {
					contentType = dbItem.Type
					if contentType == movieType {
						tmdb = strconv.Itoa(dbItem.ID)
					} else {
						show = strconv.Itoa(dbItem.ShowID)
						season = strconv.Itoa(dbItem.Season)
						episode = strconv.Itoa(dbItem.Episode)
					}
					query = dbItem.Query
				}
			}
			xbmc.PlayURL(URLQuery(URLForXBMC("/play"),
				"resume", resume,
				"index", index,
				"tmdb", tmdb,
				"show", show,
				"season", season,
				"episode", episode,
				"query", query,
				"type", contentType))
		}
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		ctx.String(200, "")
	}
}
