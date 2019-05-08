package api

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"

	"github.com/bcrusher29/solaris/database"
	"github.com/bcrusher29/solaris/library"
	"github.com/bcrusher29/solaris/scrape"
	"github.com/bcrusher29/solaris/tmdb"

	"github.com/dustin/go-humanize"
	"github.com/gin-gonic/gin"

	"github.com/bcrusher29/solaris/config"
	"github.com/bcrusher29/solaris/util"
	"github.com/bcrusher29/solaris/xbmc"
)

// Changelog display
func Changelog(ctx *gin.Context) {
	changelogPath := filepath.Join(config.Get().Info.Path, "whatsnew.txt")
	if _, err := os.Stat(changelogPath); err != nil {
		ctx.String(404, err.Error())
		return
	}

	title := "LOCALIZE[30355]"
	text, err := ioutil.ReadFile(changelogPath)
	if err != nil {
		ctx.String(404, err.Error())
		return
	}

	xbmc.DialogText(title, string(text))
	ctx.String(200, "")
}

// Donate display
func Donate(ctx *gin.Context) {
	xbmc.Dialog("Elementum", "LOCALIZE[30141]")
	ctx.String(200, "")
}

// Status display
func Status(ctx *gin.Context) {
	title := "LOCALIZE[30393]"
	text := ""

	text += `[B]LOCALIZE[30394]:[/B] %s

[B]LOCALIZE[30395]:[/B] %s
[B]LOCALIZE[30396]:[/B] %d
[B]LOCALIZE[30488]:[/B] %d

[COLOR pink][B]LOCALIZE[30399]:[/B][/COLOR]
    [B]LOCALIZE[30397]:[/B] %s
    [B]LOCALIZE[30401]:[/B] %s
    [B]LOCALIZE[30439]:[/B] %s
    [B]LOCALIZE[30398]:[/B] %s

[COLOR pink][B]LOCALIZE[30400]:[/B][/COLOR]
    [B]LOCALIZE[30403]:[/B] %s
    [B]LOCALIZE[30402]:[/B] %s

    [B]LOCALIZE[30404]:[/B] %d
    [B]LOCALIZE[30405]:[/B] %d
    [B]LOCALIZE[30458]:[/B] %d
    [B]LOCALIZE[30459]:[/B] %d
`

	ip := "127.0.0.1"
	if localIP, err := util.LocalIP(); err == nil {
		ip = localIP.String()
	}

	port := config.Args.LocalPort
	webAddress := fmt.Sprintf("http://%s:%d/web", ip, port)
	debugAllAddress := fmt.Sprintf("http://%s:%d/debug/all", ip, port)
	debugBundleAddress := fmt.Sprintf("http://%s:%d/debug/bundle", ip, port)
	infoAddress := fmt.Sprintf("http://%s:%d/info", ip, port)

	appSize := fileSize(filepath.Join(config.Get().Info.Profile, database.Get().GetFilename()))
	cacheSize := fileSize(filepath.Join(config.Get().Info.Profile, database.GetCache().GetFilename()))

	torrentsCount := 0
	queriesCount := 0
	deletedMoviesCount := 0
	deletedShowsCount := 0

	database.Get().QueryRow("SELECT COUNT(1) FROM thistory_metainfo").Scan(&torrentsCount)
	database.Get().QueryRow("SELECT COUNT(1) FROM history_queries").Scan(&queriesCount)
	database.Get().QueryRow("SELECT COUNT(1) FROM library_items WHERE state = ? AND mediaType = ?", library.StateDeleted, library.MovieType).Scan(&deletedMoviesCount)
	database.Get().QueryRow("SELECT COUNT(1) FROM library_items WHERE state = ? AND mediaType = ?", library.StateDeleted, library.ShowType).Scan(&deletedShowsCount)

	text = fmt.Sprintf(text,
		util.GetVersion(),
		ip,
		port,
		scrape.ProxyPort,

		webAddress,
		infoAddress,
		debugAllAddress,
		debugBundleAddress,

		appSize,
		cacheSize,

		torrentsCount,
		queriesCount,
		deletedMoviesCount,
		deletedShowsCount,
	)

	xbmc.DialogText(title, string(text))
	ctx.String(200, "")
}

func fileSize(path string) string {
	fi, err := os.Stat(path)
	if err != nil {
		return ""
	}

	return humanize.Bytes(uint64(fi.Size()))
}

// SelectNetworkInterface ...
func SelectNetworkInterface(ctx *gin.Context) {
	typeName := ctx.Params.ByName("type")

	ifaces, err := net.Interfaces()
	if err != nil {
		ctx.String(404, err.Error())
		return
	}

	items := make([]string, 0, len(ifaces))

	for _, i := range ifaces {
		name := i.Name
		address := ""

		addrs, err := i.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			v4 := ip.To4()
			if v4 != nil {
				address = v4.String()
			}
		}

		if address != "" {
			name = fmt.Sprintf("[B]%s[/B] (%s)", i.Name, address)
		} else {
			name = fmt.Sprintf("[B]%s[/B]", i.Name)
		}

		items = append(items, name)
	}

	choice := xbmc.ListDialog("LOCALIZE[30474]", items...)
	if choice >= 0 {
		xbmc.SetSetting("listen_autodetect_ip", "false")
		if typeName == "listen" {
			xbmc.SetSetting("listen_interfaces", ifaces[choice].Name)
		} else {
			xbmc.SetSetting("outgoing_interfaces", ifaces[choice].Name)
		}
	}

	ctx.String(200, "")
}

// SelectStrmLanguage ...
func SelectStrmLanguage(ctx *gin.Context) {
	items := make([]string, 0)
	items = append(items, xbmc.GetLocalizedString(30477))

	languages := tmdb.GetLanguages(config.Get().Language)
	for _, l := range languages {
		items = append(items, l.Name)
	}

	choice := xbmc.ListDialog("LOCALIZE[30373]", items...)
	if choice >= 1 {
		xbmc.SetSetting("strm_language", languages[choice-1].Name+" | "+languages[choice-1].Iso639_1)
	} else if choice == 0 {
		xbmc.SetSetting("strm_language", "Original")
	}

	ctx.String(200, "")
}
