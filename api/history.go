package api

import (
	"fmt"

	"github.com/bcrusher29/solaris/database"
	"github.com/bcrusher29/solaris/xbmc"

	"github.com/gin-gonic/gin"
)

// History ...
func History(ctx *gin.Context) {
	ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")

	infohash := ctx.Query("infohash")
	if torrent := InTorrentsHistory(infohash); torrent != nil {
		xbmc.PlayURLWithTimeout(URLQuery(
			URLForXBMC("/play"), "uri", torrent.URI,
		))
		return
	}

	rows, err := database.Get().Query(`SELECT name, infohash FROM torrent_history ORDER BY dt DESC`)
	if err != nil {
		ctx.Error(err)
		return
	}

	defer rows.Close()

	name := ""
	infohash = ""
	items := []*xbmc.ListItem{}
	for rows.Next() {
		rows.Scan(&name, &infohash)

		items = append(items, &xbmc.ListItem{
			Label: name,
			Path:  torrentHistoryGetXbmcURL(infohash),
			ContextMenu: [][]string{
				[]string{"LOCALIZE[30406]", fmt.Sprintf("XBMC.RunPlugin(%s)",
					URLQuery(URLForXBMC("/history/remove"),
						"infohash", infohash,
					))},
			},
		})
	}

	ctx.JSON(200, xbmc.NewView("", items))
}

func torrentHistoryEmpty() bool {
	count := 0
	err := database.Get().QueryRow("SELECT COUNT(*) FROM torrent_history").Scan(&count)

	return err != nil || count == 0
}

// HistoryRemove ...
func HistoryRemove(ctx *gin.Context) {
	infohash := ctx.DefaultQuery("infohash", "")

	if len(infohash) == 0 {
		return
	}

	log.Debugf("Removing infohash '%s' with torrent history", infohash)
	database.Get().Exec("DELETE FROM torrent_history WHERE infohash = ?", infohash)
	xbmc.Refresh()

	ctx.String(200, "")
	return
}

// HistoryClear ...
func HistoryClear(ctx *gin.Context) {
	log.Debugf("Cleaning queries with torrent history")
	database.Get().Exec("DELETE FROM torrent_history")
	xbmc.Refresh()

	ctx.String(200, "")
	return
}

func torrentHistoryGetXbmcURL(infohash string) string {
	return URLQuery(URLForXBMC("/history"), "infohash", infohash)
}

func torrentHistoryGetHTTPUrl(infohash string) string {
	return URLQuery(URLForHTTP("/history"), "infohash", infohash)
}
