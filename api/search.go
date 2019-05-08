package api

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/bcrusher29/solaris/bittorrent"
	"github.com/bcrusher29/solaris/config"
	"github.com/bcrusher29/solaris/database"
	"github.com/bcrusher29/solaris/providers"
	"github.com/bcrusher29/solaris/xbmc"

	"github.com/cespare/xxhash"
	"github.com/gin-gonic/gin"
	"github.com/op/go-logging"
)

var searchLog = logging.MustGetLogger("search")

// Search ...
func Search(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		query := ctx.Query("q")
		keyboard := ctx.Query("keyboard")

		if len(query) == 0 {
			historyType := ""
			if len(keyboard) > 0 {
				if query = xbmc.Keyboard("", "LOCALIZE[30206]"); len(query) == 0 {
					return
				}
				searchHistoryAppend(ctx, historyType, query)
			} else {
				searchHistoryList(ctx, historyType)
			}

			return
		}

		fakeTmdbID := strconv.FormatUint(xxhash.Sum64String(query), 10)
		existingTorrent := s.HasTorrentByQuery(query)
		if existingTorrent != "" && (config.Get().SilentStreamStart || xbmc.DialogConfirmFocused("Elementum", "LOCALIZE[30270]")) {
			xbmc.PlayURLWithTimeout(URLQuery(
				URLForXBMC("/play"),
				"resume", existingTorrent,
				"query", query,
				"tmdb", fakeTmdbID,
				"type", "search"))
			return
		}

		if torrent := InTorrentsMap(fakeTmdbID); torrent != nil {
			xbmc.PlayURLWithTimeout(URLQuery(
				URLForXBMC("/play"), "uri", torrent.URI,
				"query", query,
				"tmdb", fakeTmdbID,
				"type", "search"))
			return
		}

		var torrents []*bittorrent.TorrentFile
		var err error

		if torrents, err = GetCachedTorrents(fakeTmdbID); err != nil || len(torrents) == 0 {
			searchLog.Infof("Searching providers for: %s", query)

			searchers := providers.GetSearchers()
			torrents = providers.Search(searchers, query)

			SetCachedTorrents(fakeTmdbID, torrents)
		}

		if len(torrents) == 0 {
			xbmc.Notify("Elementum", "LOCALIZE[30205]", config.AddonIcon())
			return
		}

		choices := make([]string, 0, len(torrents))
		for _, torrent := range torrents {
			resolution := ""
			if torrent.Resolution > 0 {
				resolution = fmt.Sprintf("[B][COLOR %s]%s[/COLOR][/B] ", bittorrent.Colors[torrent.Resolution], bittorrent.Resolutions[torrent.Resolution])
			}

			info := make([]string, 0)
			if torrent.Size != "" {
				info = append(info, fmt.Sprintf("[B][%s][/B]", torrent.Size))
			}
			if torrent.RipType > 0 {
				info = append(info, bittorrent.Rips[torrent.RipType])
			}
			if torrent.VideoCodec > 0 {
				info = append(info, bittorrent.Codecs[torrent.VideoCodec])
			}
			if torrent.AudioCodec > 0 {
				info = append(info, bittorrent.Codecs[torrent.AudioCodec])
			}
			if torrent.Provider != "" {
				info = append(info, fmt.Sprintf(" - [B]%s[/B]", torrent.Provider))
			}

			multi := ""
			if torrent.Multi {
				multi = multiType
			}

			label := fmt.Sprintf("%s(%d / %d) %s\n%s\n%s%s",
				resolution,
				torrent.Seeds,
				torrent.Peers,
				strings.Join(info, " "),
				torrent.Name,
				torrent.Icon,
				multi,
			)
			choices = append(choices, label)
		}

		choice := xbmc.ListDialogLarge("LOCALIZE[30228]", query, choices...)
		if choice >= 0 {
			AddToTorrentsMap(fakeTmdbID, torrents[choice])

			xbmc.PlayURLWithTimeout(URLQuery(
				URLForXBMC("/play"),
				"uri", torrents[choice].URI,
				"query", query,
				"tmdb", fakeTmdbID,
				"type", "search"))
			return
		}
	}
}

func searchHistoryEmpty(historyType string) bool {
	count := 0
	err := database.Get().QueryRow("SELECT COUNT(*) FROM history_queries WHERE type = ?", historyType).Scan(&count)

	return err != nil || count == 0
}

func searchHistoryAppend(ctx *gin.Context, historyType string, query string) {
	database.Get().AddSearchHistory(historyType, query)

	go xbmc.UpdatePath(searchHistoryGetXbmcURL(historyType, query))
	ctx.String(200, "")
	return
}

func searchHistoryList(ctx *gin.Context, historyType string) {
	historyList := []string{}
	rows, err := database.Get().Query(`SELECT query FROM history_queries WHERE type = ? ORDER BY dt DESC`, historyType)
	if err != nil {
		ctx.Error(err)
		return
	}

	query := ""
	for rows.Next() {
		rows.Scan(&query)
		historyList = append(historyList, query)
	}
	rows.Close()

	urlPrefix := ""
	if len(historyType) > 0 {
		urlPrefix = "/" + historyType
	}

	items := make(xbmc.ListItems, 0, len(historyList)+1)
	items = append(items, &xbmc.ListItem{
		Label:     "LOCALIZE[30323]",
		Path:      URLQuery(URLForXBMC(urlPrefix+"/search"), "keyboard", "1"),
		Thumbnail: config.AddonResource("img", "search.png"),
		Icon:      config.AddonResource("img", "search.png"),
	})

	for _, query := range historyList {
		items = append(items, &xbmc.ListItem{
			Label: query,
			Path:  searchHistoryGetXbmcURL(historyType, query),
			ContextMenu: [][]string{
				[]string{"LOCALIZE[30406]", fmt.Sprintf("XBMC.RunPlugin(%s)",
					URLQuery(URLForXBMC("/search/remove"),
						"query", query,
						"type", historyType,
					))},
			},
		})
	}

	ctx.JSON(200, xbmc.NewView("", items))
}

// SearchRemove ...
func SearchRemove(ctx *gin.Context) {
	query := ctx.DefaultQuery("query", "")
	historyType := ctx.DefaultQuery("type", "")

	if len(query) == 0 {
		return
	}

	log.Debugf("Removing query '%s' with history type '%s'", query, historyType)
	database.Get().Exec("DELETE FROM history_queries WHERE query = ? AND type = ?", query, historyType)
	xbmc.Refresh()

	ctx.String(200, "")
	return
}

// SearchClear ...
func SearchClear(ctx *gin.Context) {
	historyType := ctx.DefaultQuery("type", "")

	log.Debugf("Cleaning queries with history type %s", historyType)
	database.Get().Exec("DELETE FROM history_queries WHERE type = ?", historyType)
	xbmc.Refresh()

	ctx.String(200, "")
	return
}

func searchHistoryGetXbmcURL(historyType string, query string) string {
	urlPrefix := ""
	if len(historyType) > 0 {
		urlPrefix = "/" + historyType
	}

	return URLQuery(URLForXBMC(urlPrefix+"/search"), "q", query)
}

func searchHistoryGetHTTPUrl(historyType string, query string) string {
	urlPrefix := ""
	if len(historyType) > 0 {
		urlPrefix = "/" + historyType
	}

	return URLQuery(URLForHTTP(urlPrefix+"/search"), "q", query)
}
