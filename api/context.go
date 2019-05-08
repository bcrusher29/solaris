package api

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/bcrusher29/solaris/bittorrent"
	"github.com/bcrusher29/solaris/config"
	"github.com/bcrusher29/solaris/library"
)

// ContextPlaySelector ...
func ContextPlaySelector(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")

		id := ctx.Params.ByName("kodiID")
		kodiID, _ := strconv.Atoi(id)
		media := ctx.Params.ByName("media")

		action := "forcelinks"
		if config.Get().ChooseStreamAuto {
			action = "forceplay"
		}

		if kodiID == 0 {
			ctx.Redirect(302, URLQuery(URLForXBMC("/search"), "q", id))
			return
		} else if media == "movie" {
			if m := library.GetLibraryMovie(kodiID); m != nil && m.UIDs.TMDB != 0 {
				title := fmt.Sprintf("%s (%d)", m.Title, m.Year)
				ctx.Redirect(302, URLQuery(URLForXBMC("/movie/%d/%s/%s", m.UIDs.TMDB, action, url.PathEscape(title))))
				return
			}
		} else if media == "episode" {
			if s, e := library.GetLibraryEpisode(kodiID); s != nil && e != nil && e.UIDs.TMDB != 0 {
				title := fmt.Sprintf("%s S%dE%d", s.Title, e.Season, e.Episode)
				ctx.Redirect(302, URLQuery(URLForXBMC("/show/%d/season/%d/episode/%d/%s/%s", s.UIDs.TMDB, e.Season, e.Episode, action, url.PathEscape(title))))
				return
			}
		}

		log.Debugf("Cound not find TMDB entry for requested Kodi item %d of type %s", kodiID, media)
		ctx.String(404, "Cannot find TMDB for selected Kodi item")
		return
	}
}
