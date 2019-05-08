package api

import (
	"fmt"
	"net/url"

	"github.com/bcrusher29/solaris/config"
	"github.com/bcrusher29/solaris/util"
	"github.com/bcrusher29/solaris/xbmc"
)

// type contextMenu []*contextMenuItem
//
// type contextMenuItem []string
//
// // contextMenuRequest ...
// type contextMenuRequest struct {
// }
//
// func makeContextMenu(r contextMenuRequest) *contextMenu {
// 	m := &contextMenu{}
//
// }

func filterListItems(l xbmc.ListItems) xbmc.ListItems {
	t := config.Get().TraktToken != ""

	ret := make(xbmc.ListItems, 0)
	for _, i := range l {
		if !i.TraktAuth || t {
			ret = append(ret, i)
		}
	}

	return ret
}

// URLForHTTP ...
func URLForHTTP(pattern string, args ...interface{}) string {
	u, _ := url.Parse(fmt.Sprintf(pattern, args...))
	return util.GetHTTPHost() + u.String()
}

// URLForXBMC ...
func URLForXBMC(pattern string, args ...interface{}) string {
	u, _ := url.Parse(fmt.Sprintf(pattern, args...))
	return "plugin://" + config.Get().Info.ID + u.String()
}

// URLQuery ...
func URLQuery(route string, query ...string) string {
	v := url.Values{}
	for i := 0; i < len(query); i += 2 {
		v.Add(query[i], query[i+1])
	}
	return route + "?" + v.Encode()
}

func contextPlayURL(f string, title string, forced bool) string {
	action := "links"
	if config.Get().ChooseStreamAuto {
		action = "play"
	}
	if forced {
		action = "force" + action
	}

	return fmt.Sprintf(f, action, url.PathEscape(title))
}

func contextPlayOppositeURL(f string, title string, forced bool) string {
	action := "links"
	if !config.Get().ChooseStreamAuto {
		action = "play"
	}
	if forced {
		action = "force" + action
	}

	return fmt.Sprintf(f, action, url.PathEscape(title))
}
