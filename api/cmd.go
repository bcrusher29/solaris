package api

import (
	"github.com/gin-gonic/gin"
	"github.com/op/go-logging"

	"github.com/bcrusher29/solaris/config"
	"github.com/bcrusher29/solaris/database"
	"github.com/bcrusher29/solaris/library"
	"github.com/bcrusher29/solaris/xbmc"
)

var cmdLog = logging.MustGetLogger("cmd")

// ClearCache ...
func ClearCache(ctx *gin.Context) {
	key := ctx.Params.ByName("key")
	if key != "" {
		if ctx != nil {
			ctx.Abort()
		}

		library.ClearCacheKey(key)

	} else {
		log.Debug("Removing all the cache")

		if !xbmc.DialogConfirm("Elementum", "LOCALIZE[30471]") {
			ctx.String(200, "")
			return
		}

		database.GetCache().RecreateBucket(database.CommonBucket)
	}

	xbmc.Notify("Elementum", "LOCALIZE[30200]", config.AddonIcon())
}

// ClearCacheTMDB ...
func ClearCacheTMDB(ctx *gin.Context) {
	log.Debug("Removing TMDB cache")

	library.ClearTmdbCache()

	xbmc.Notify("Elementum", "LOCALIZE[30200]", config.AddonIcon())
}

// ClearCacheTrakt ...
func ClearCacheTrakt(ctx *gin.Context) {
	log.Debug("Removing Trakt cache")

	library.ClearTraktCache()

	xbmc.Notify("Elementum", "LOCALIZE[30200]", config.AddonIcon())
}

// ClearPageCache ...
func ClearPageCache(ctx *gin.Context) {
	if ctx != nil {
		ctx.Abort()
	}
	library.ClearPageCache()
}

// ClearTraktCache ...
func ClearTraktCache(ctx *gin.Context) {
	if ctx != nil {
		ctx.Abort()
	}
	library.ClearTraktCache()
}

// ClearTmdbCache ...
func ClearTmdbCache(ctx *gin.Context) {
	if ctx != nil {
		ctx.Abort()
	}
	library.ClearTmdbCache()
}

// ResetPath ...
func ResetPath(ctx *gin.Context) {
	xbmc.SetSetting("download_path", "")
	xbmc.SetSetting("library_path", "special://temp/elementum_library/")
	xbmc.SetSetting("torrents_path", "special://temp/elementum_torrents/")
}

// ResetCustomPath ...
func ResetCustomPath(ctx *gin.Context) {
	path := ctx.Params.ByName("path")

	if path != "" {
		xbmc.SetSetting(path + "_path", "/")
	}
}

// SetViewMode ...
func SetViewMode(ctx *gin.Context) {
	contentType := ctx.Params.ByName("content_type")
	viewName := xbmc.InfoLabel("Container.Viewmode")
	viewMode := xbmc.GetCurrentView()
	cmdLog.Noticef("ViewMode: %s (%s)", viewName, viewMode)
	if viewMode != "0" {
		xbmc.SetSetting("viewmode_"+contentType, viewMode)
	}
	ctx.String(200, "")
}

// ClearDatabaseMovies ...
func ClearDatabaseMovies(ctx *gin.Context) {
	log.Debug("Removing deleted movies from database")

	database.Get().Exec("DELETE FROM library_items WHERE state = ? AND mediaType = ?", library.StateDeleted, library.MovieType)

	xbmc.Notify("Elementum", "LOCALIZE[30472]", config.AddonIcon())

	ctx.String(200, "")
	return

}

// ClearDatabaseShows ...
func ClearDatabaseShows(ctx *gin.Context) {
	log.Debug("Removing deleted shows from database")

	database.Get().Exec("DELETE FROM library_items WHERE state = ? AND mediaType = ?", library.StateDeleted, library.ShowType)

	xbmc.Notify("Elementum", "LOCALIZE[30472]", config.AddonIcon())

	ctx.String(200, "")
	return

}

// ClearDatabaseTorrentHistory ...
func ClearDatabaseTorrentHistory(ctx *gin.Context) {
	log.Debug("Removing torrent history from database")

	database.Get().Exec("DELETE FROM thistory_assign")
	database.Get().Exec("DELETE FROM thistory_metainfo")
	database.Get().Exec("DELETE FROM tinfo")

	xbmc.Notify("Elementum", "LOCALIZE[30472]", config.AddonIcon())

	ctx.String(200, "")
	return

}

// ClearDatabaseSearchHistory ...
func ClearDatabaseSearchHistory(ctx *gin.Context) {
	log.Debug("Removing search history from database")

	database.Get().Exec("DELETE FROM history_queries")

	xbmc.Notify("Elementum", "LOCALIZE[30472]", config.AddonIcon())

	ctx.String(200, "")
	return

}

// ClearDatabase ...
func ClearDatabase(ctx *gin.Context) {
	log.Debug("Removing all the database")

	if !xbmc.DialogConfirm("Elementum", "LOCALIZE[30471]") {
		ctx.String(200, "")
		return
	}

	database.Get().Exec("DELETE FROM history_queries")
	database.Get().Exec("DELETE FROM library_items")
	database.Get().Exec("DELETE FROM library_uids")
	database.Get().Exec("DELETE FROM thistory_assign")
	database.Get().Exec("DELETE FROM thistory_metainfo")
	database.Get().Exec("DELETE FROM tinfo")

	xbmc.Notify("Elementum", "LOCALIZE[30472]", config.AddonIcon())

	ctx.String(200, "")
	return
}
