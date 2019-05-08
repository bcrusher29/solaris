package api

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/bcrusher29/solaris/bittorrent"
	"github.com/bcrusher29/solaris/config"
	"github.com/bcrusher29/solaris/library"
	"github.com/bcrusher29/solaris/providers"
	"github.com/bcrusher29/solaris/tmdb"
	"github.com/bcrusher29/solaris/trakt"
	"github.com/bcrusher29/solaris/xbmc"
	"github.com/gin-gonic/gin"
)

// TVIndex ...
func TVIndex(ctx *gin.Context) {
	items := xbmc.ListItems{
		{Label: "LOCALIZE[30209]", Path: URLForXBMC("/shows/search"), Thumbnail: config.AddonResource("img", "search.png")},

		{Label: "LOCALIZE[30360]", Path: URLForXBMC("/shows/trakt/progress"), Thumbnail: config.AddonResource("img", "trakt.png"), TraktAuth: true},
		{Label: "LOCALIZE[30263]", Path: URLForXBMC("/shows/trakt/lists/"), Thumbnail: config.AddonResource("img", "trakt.png"), TraktAuth: true},
		{Label: "LOCALIZE[30254]", Path: URLForXBMC("/shows/trakt/watchlist"), Thumbnail: config.AddonResource("img", "trakt.png"), ContextMenu: [][]string{[]string{"LOCALIZE[30252]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/library/show/list/add/watchlist"))}}, TraktAuth: true},
		{Label: "LOCALIZE[30257]", Path: URLForXBMC("/shows/trakt/collection"), Thumbnail: config.AddonResource("img", "trakt.png"), ContextMenu: [][]string{[]string{"LOCALIZE[30252]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/library/show/list/add/collection"))}}, TraktAuth: true},
		{Label: "LOCALIZE[30290]", Path: URLForXBMC("/shows/trakt/calendars/"), Thumbnail: config.AddonResource("img", "most_anticipated.png"), TraktAuth: true},
		{Label: "LOCALIZE[30423]", Path: URLForXBMC("/shows/trakt/recommendations"), Thumbnail: config.AddonResource("img", "tv.png"), TraktAuth: true},
		{Label: "LOCALIZE[30246]", Path: URLForXBMC("/shows/trakt/trending"), Thumbnail: config.AddonResource("img", "trending.png")},
		{Label: "LOCALIZE[30210]", Path: URLForXBMC("/shows/trakt/popular"), Thumbnail: config.AddonResource("img", "popular.png")},
		{Label: "LOCALIZE[30247]", Path: URLForXBMC("/shows/trakt/played"), Thumbnail: config.AddonResource("img", "most_played.png")},
		{Label: "LOCALIZE[30248]", Path: URLForXBMC("/shows/trakt/watched"), Thumbnail: config.AddonResource("img", "most_watched.png")},
		{Label: "LOCALIZE[30249]", Path: URLForXBMC("/shows/trakt/collected"), Thumbnail: config.AddonResource("img", "most_collected.png")},
		{Label: "LOCALIZE[30250]", Path: URLForXBMC("/shows/trakt/anticipated"), Thumbnail: config.AddonResource("img", "most_anticipated.png")},

		{Label: "LOCALIZE[30238]", Path: URLForXBMC("/shows/recent/episodes"), Thumbnail: config.AddonResource("img", "fresh.png")},
		{Label: "LOCALIZE[30237]", Path: URLForXBMC("/shows/recent/shows"), Thumbnail: config.AddonResource("img", "clock.png")},
		{Label: "LOCALIZE[30210]", Path: URLForXBMC("/shows/popular"), Thumbnail: config.AddonResource("img", "popular.png")},
		{Label: "LOCALIZE[30211]", Path: URLForXBMC("/shows/top"), Thumbnail: config.AddonResource("img", "top_rated.png")},
		{Label: "LOCALIZE[30212]", Path: URLForXBMC("/shows/mostvoted"), Thumbnail: config.AddonResource("img", "most_voted.png")},
		{Label: "LOCALIZE[30289]", Path: URLForXBMC("/shows/genres"), Thumbnail: config.AddonResource("img", "genre_comedy.png")},
		{Label: "LOCALIZE[30373]", Path: URLForXBMC("/shows/languages"), Thumbnail: config.AddonResource("img", "genre_tv.png")},
		// Note: Search by countries is implemented, but TMDB does not support it yet,
		// so we are not showing this. When there is an endpoint - we can enable
		// and modify the URL params to /discover endpoint
		// {Label: "LOCALIZE[30374]", Path: URLForXBMC("/shows/countries"), Thumbnail: config.AddonResource("img", "genre_tv.png")},

		{Label: "LOCALIZE[30361]", Path: URLForXBMC("/shows/trakt/history"), Thumbnail: config.AddonResource("img", "trakt.png"), TraktAuth: true},

		{Label: "LOCALIZE[30517]", Path: URLForXBMC("/shows/library"), Thumbnail: config.AddonResource("img", "genre_tv.png")},
	}
	for _, item := range items {
		item.ContextMenu = [][]string{
			[]string{"LOCALIZE[30143]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/setviewmode/menus_tvshows"))},
		}
	}

	// Adding items from custom menu
	if TVMenu.AddItems != nil && len(TVMenu.AddItems) > 0 {
		index := 1
		for _, i := range TVMenu.AddItems {
			item := &xbmc.ListItem{Label: i.Name, Path: i.Link, Thumbnail: config.AddonResource("img", "genre_tv.png")}
			item.ContextMenu = [][]string{
				[]string{"LOCALIZE[30521]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLQuery(URLForXBMC("/menu/tv/remove"), "name", i.Name, "link", i.Link))},
			}

			items = append(items[:index], append([]*xbmc.ListItem{item}, items[index:]...)...)
			index++
		}
	}

	ctx.JSON(200, xbmc.NewView("menus_tvshows", filterListItems(items)))
}

// TVGenres ...
func TVGenres(ctx *gin.Context) {
	items := make(xbmc.ListItems, 0)
	for _, genre := range tmdb.GetTVGenres(config.Get().Language) {
		slug, _ := genreSlugs[genre.ID]
		items = append(items, &xbmc.ListItem{
			Label:     genre.Name,
			Path:      URLForXBMC("/shows/popular/genre/%s", strconv.Itoa(genre.ID)),
			Thumbnail: config.AddonResource("img", fmt.Sprintf("genre_%s.png", slug)),
			ContextMenu: [][]string{
				[]string{"LOCALIZE[30237]", fmt.Sprintf("Container.Update(%s)", URLForXBMC("/shows/recent/shows/genre/%s", strconv.Itoa(genre.ID)))},
				[]string{"LOCALIZE[30238]", fmt.Sprintf("Container.Update(%s)", URLForXBMC("/shows/recent/episodes/genre/%s", strconv.Itoa(genre.ID)))},
				[]string{"LOCALIZE[30144]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/setviewmode/menus_tvshows_genres"))},
			},
		})
	}
	ctx.JSON(200, xbmc.NewView("menus_tvshows_genres", filterListItems(items)))
}

// TVLanguages ...
func TVLanguages(ctx *gin.Context) {
	items := make(xbmc.ListItems, 0)
	for _, language := range tmdb.GetLanguages(config.Get().Language) {
		items = append(items, &xbmc.ListItem{
			Label: language.Name,
			Path:  URLForXBMC("/shows/popular/language/%s", language.Iso639_1),
			ContextMenu: [][]string{
				[]string{"LOCALIZE[30237]", fmt.Sprintf("Container.Update(%s)", URLForXBMC("/shows/recent/shows/language/%s", language.Iso639_1))},
				[]string{"LOCALIZE[30238]", fmt.Sprintf("Container.Update(%s)", URLForXBMC("/shows/recent/episodes/language/%s", language.Iso639_1))},
				[]string{"LOCALIZE[30144]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/setviewmode/menus_tvshows_languages"))},
			},
		})
	}
	ctx.JSON(200, xbmc.NewView("menus_tvshows_languages", filterListItems(items)))
}

// TVCountries ...
func TVCountries(ctx *gin.Context) {
	items := make(xbmc.ListItems, 0)
	for _, country := range tmdb.GetCountries(config.Get().Language) {
		items = append(items, &xbmc.ListItem{
			Label: country.EnglishName,
			Path:  URLForXBMC("/shows/popular/country/%s", country.Iso31661),
			ContextMenu: [][]string{
				[]string{"LOCALIZE[30237]", fmt.Sprintf("Container.Update(%s)", URLForXBMC("/shows/recent/shows/country/%s", country.Iso31661))},
				[]string{"LOCALIZE[30238]", fmt.Sprintf("Container.Update(%s)", URLForXBMC("/shows/recent/episodes/country/%s", country.Iso31661))},
				[]string{"LOCALIZE[30144]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/setviewmode/menus_tvshows_countries"))},
			},
		})
	}
	ctx.JSON(200, xbmc.NewView("menus_tvshows_countries", filterListItems(items)))
}

// TVLibrary ...
func TVLibrary(ctx *gin.Context) {
	shows, err := xbmc.VideoLibraryGetElementumShows()
	if err != nil || shows == nil || shows.Limits == nil || shows.Limits.Total == 0 {
		return
	}

	tmdbShows := tmdb.Shows{}
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))

	for i := (page - 1) * config.Get().ResultsPerPage; i < shows.Limits.Total && i < page*config.Get().ResultsPerPage; i++ {
		if shows == nil || shows.Shows == nil || len(shows.Shows) < i {
			continue
		}

		if id, err := strconv.Atoi(shows.Shows[i].UniqueIDs.Elementum); err == nil {
			s := tmdb.GetShow(id, config.Get().Language)
			if s != nil {
				tmdbShows = append(tmdbShows, s)
			}
		}
	}

	renderShows(ctx, tmdbShows, page, shows.Limits.Total, "")
}

// TVTraktLists ...
func TVTraktLists(ctx *gin.Context) {
	items := xbmc.ListItems{}

	lists := trakt.Userlists()
	lists = append(lists, trakt.Likedlists()...)

	sort.Slice(lists, func(i int, j int) bool {
		return lists[i].Name < lists[j].Name
	})

	for _, list := range lists {
		link := URLForXBMC("/shows/trakt/lists/%s/%d", list.User.Ids.Slug, list.IDs.Trakt)
		menuItem := []string{"LOCALIZE[30520]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLQuery(URLForXBMC("/menu/shows/add"), "name", list.Name, "link", link))}
		if MovieMenu.Contains(addAction, &MenuItem{Name: list.Name, Link: link}) {
			menuItem = []string{"LOCALIZE[30521]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLQuery(URLForXBMC("/menu/shows/remove"), "name", list.Name, "link", link))}
		}

		item := &xbmc.ListItem{
			Label:     list.Name,
			Path:      link,
			Thumbnail: config.AddonResource("img", "trakt.png"),
			ContextMenu: [][]string{
				menuItem,
			},
		}
		items = append(items, item)
	}

	ctx.JSON(200, xbmc.NewView("menus_tvshows", filterListItems(items)))
}

// CalendarShows ...
func CalendarShows(ctx *gin.Context) {
	items := xbmc.ListItems{
		{Label: "LOCALIZE[30295]", Path: URLForXBMC("/shows/trakt/calendars/shows"), Thumbnail: config.AddonResource("img", "tv.png")},
		{Label: "LOCALIZE[30296]", Path: URLForXBMC("/shows/trakt/calendars/newshows"), Thumbnail: config.AddonResource("img", "fresh.png")},
		{Label: "LOCALIZE[30297]", Path: URLForXBMC("/shows/trakt/calendars/premieres"), Thumbnail: config.AddonResource("img", "box_office.png")},
		{Label: "LOCALIZE[30298]", Path: URLForXBMC("/shows/trakt/calendars/allshows"), Thumbnail: config.AddonResource("img", "tv.png")},
		{Label: "LOCALIZE[30299]", Path: URLForXBMC("/shows/trakt/calendars/allnewshows"), Thumbnail: config.AddonResource("img", "fresh.png")},
		{Label: "LOCALIZE[30300]", Path: URLForXBMC("/shows/trakt/calendars/allpremieres"), Thumbnail: config.AddonResource("img", "box_office.png")},
	}
	ctx.JSON(200, xbmc.NewView("menus_tvshows", filterListItems(items)))
}

func renderShows(ctx *gin.Context, shows tmdb.Shows, page int, total int, query string) {
	hasNextPage := 0
	if page > 0 {
		if page*config.Get().ResultsPerPage < total {
			hasNextPage = 1
		}
	}

	items := make(xbmc.ListItems, 0, len(shows)+hasNextPage)

	for _, show := range shows {
		if show == nil {
			continue
		}
		item := show.ToListItem()
		item.Path = URLForXBMC("/show/%d/seasons", show.ID)

		tmdbID := strconv.Itoa(show.ID)
		libraryActions := [][]string{}
		if err := library.IsDuplicateShow(tmdbID); err != nil || library.IsAddedToLibrary(tmdbID, library.ShowType) {
			libraryActions = append(libraryActions, []string{"LOCALIZE[30283]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/library/show/add/%d?force=true", show.ID))})
			libraryActions = append(libraryActions, []string{"LOCALIZE[30253]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/library/show/remove/%d", show.ID))})
		} else {
			libraryActions = append(libraryActions, []string{"LOCALIZE[30252]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/library/show/add/%d", show.ID))})
		}

		watchlistAction := []string{"LOCALIZE[30255]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/show/%d/watchlist/add", show.ID))}
		if inShowsWatchlist(show.ID) {
			watchlistAction = []string{"LOCALIZE[30256]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/show/%d/watchlist/remove", show.ID))}
		}

		collectionAction := []string{"LOCALIZE[30258]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/show/%d/collection/add", show.ID))}
		if inShowsCollection(show.ID) {
			collectionAction = []string{"LOCALIZE[30259]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/show/%d/collection/remove", show.ID))}
		}

		item.ContextMenu = [][]string{
			watchlistAction,
			collectionAction,
			[]string{"LOCALIZE[30035]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/setviewmode/tvshows"))},
		}
		item.ContextMenu = append(libraryActions, item.ContextMenu...)

		if config.Get().Platform.Kodi < 17 {
			item.ContextMenu = append(item.ContextMenu,
				[]string{"LOCALIZE[30203]", "XBMC.Action(Info)"},
				[]string{"LOCALIZE[30268]", "XBMC.Action(ToggleWatched)"},
			)
		}
		items = append(items, item)
	}
	if page >= 0 && hasNextPage > 0 {
		path := ctx.Request.URL.Path
		nextPath := URLForXBMC(fmt.Sprintf("%s?page=%d", path, page+1))
		if query != "" {
			nextPath = URLForXBMC(fmt.Sprintf("%s?q=%s&page=%d", path, query, page+1))
		}
		next := &xbmc.ListItem{
			Label:     "LOCALIZE[30415];;" + strconv.Itoa(page+1),
			Path:      nextPath,
			Thumbnail: config.AddonResource("img", "nextpage.png"),
		}
		items = append(items, next)
	}
	ctx.JSON(200, xbmc.NewView("tvshows", filterListItems(items)))
}

// PopularShows ...
func PopularShows(ctx *gin.Context) {
	p := tmdb.DiscoverFilters{}
	p.Genre = ctx.Params.ByName("genre")
	p.Language = ctx.Params.ByName("language")
	p.Country = ctx.Params.ByName("country")
	if p.Genre == "0" {
		p.Genre = ""
	}

	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	shows, total := tmdb.PopularShows(p, config.Get().Language, page)
	renderShows(ctx, shows, page, total, "")
}

// RecentShows ...
func RecentShows(ctx *gin.Context) {
	p := tmdb.DiscoverFilters{}
	p.Genre = ctx.Params.ByName("genre")
	p.Language = ctx.Params.ByName("language")
	p.Country = ctx.Params.ByName("country")
	if p.Genre == "0" {
		p.Genre = ""
	}

	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	shows, total := tmdb.RecentShows(p, config.Get().Language, page)
	renderShows(ctx, shows, page, total, "")
}

// RecentEpisodes ...
func RecentEpisodes(ctx *gin.Context) {
	p := tmdb.DiscoverFilters{}
	p.Genre = ctx.Params.ByName("genre")
	p.Language = ctx.Params.ByName("language")
	p.Country = ctx.Params.ByName("country")
	if p.Genre == "0" {
		p.Genre = ""
	}

	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	shows, total := tmdb.RecentEpisodes(p, config.Get().Language, page)
	renderShows(ctx, shows, page, total, "")
}

// TopRatedShows ...
func TopRatedShows(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	shows, total := tmdb.TopRatedShows("", config.Get().Language, page)
	renderShows(ctx, shows, page, total, "")
}

// TVMostVoted ...
func TVMostVoted(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	shows, total := tmdb.MostVotedShows("", config.Get().Language, page)
	renderShows(ctx, shows, page, total, "")
}

// SearchShows ...
func SearchShows(ctx *gin.Context) {
	ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
	query := ctx.Query("q")
	keyboard := ctx.Query("keyboard")

	if len(query) == 0 {
		historyType := "shows"
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

	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	shows, total := tmdb.SearchShows(query, config.Get().Language, page)
	renderShows(ctx, shows, page, total, query)
}

// ShowSeasons ...
func ShowSeasons(ctx *gin.Context) {
	ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
	showID, _ := strconv.Atoi(ctx.Params.ByName("showId"))

	show := tmdb.GetShow(showID, config.Get().Language)

	if show == nil {
		ctx.Error(errors.New("Unable to find show"))
		return
	}

	items := show.Seasons.ToListItems(show)
	reversedItems := make(xbmc.ListItems, 0)
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]

		thisURL := URLForXBMC("/show/%d/season/%d/", show.ID, item.Info.Season) + "%s/%s"
		contextTitle := fmt.Sprintf("%s S%d", show.OriginalName, item.Info.Season)
		contextLabel := playLabel
		contextOppositeLabel := linksLabel
		contextURL := contextPlayOppositeURL(thisURL, contextTitle, false)
		contextOppositeURL := contextPlayURL(thisURL, contextTitle, false)
		if config.Get().ChooseStreamAuto {
			contextLabel = linksLabel
			contextOppositeLabel = playLabel
		}

		item.Path = URLForXBMC("/show/%d/season/%d/episodes", show.ID, item.Info.Season)
		item.ContextMenu = [][]string{
			[]string{contextLabel, fmt.Sprintf("XBMC.PlayMedia(%s)", contextURL)},
			[]string{contextOppositeLabel, fmt.Sprintf("XBMC.PlayMedia(%s)", contextOppositeURL)},
			[]string{"LOCALIZE[30036]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/setviewmode/seasons"))},
		}
		reversedItems = append(reversedItems, item)
	}
	// xbmc.ListItems always returns false to Less() so that order is unchanged

	ctx.JSON(200, xbmc.NewView("seasons", filterListItems(reversedItems)))
}

// ShowEpisodes ...
func ShowEpisodes(ctx *gin.Context) {
	ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
	showID, _ := strconv.Atoi(ctx.Params.ByName("showId"))
	seasonNumber, _ := strconv.Atoi(ctx.Params.ByName("season"))
	language := config.Get().Language

	show := tmdb.GetShow(showID, language)
	if show == nil {
		ctx.Error(errors.New("Unable to find show"))
		return
	}

	season := tmdb.GetSeason(showID, seasonNumber, language)
	if season == nil {
		ctx.Error(errors.New("Unable to find season"))
		return
	}

	items := season.Episodes.ToListItems(show, season)

	for _, item := range items {
		thisURL := URLForXBMC("/show/%d/season/%d/episode/%d/",
			show.ID,
			seasonNumber,
			item.Info.Episode,
		) + "%s/%s"
		contextLabel := playLabel
		contextTitle := fmt.Sprintf("%s S%dE%d", show.OriginalName, seasonNumber, item.Info.Episode)
		contextURL := contextPlayOppositeURL(thisURL, contextTitle, false)
		if config.Get().ChooseStreamAuto {
			contextLabel = linksLabel
		}

		item.Path = contextPlayURL(thisURL, contextTitle, false)

		if config.Get().Platform.Kodi < 17 {
			item.ContextMenu = [][]string{
				[]string{contextLabel, fmt.Sprintf("XBMC.PlayMedia(%s)", contextURL)},
				[]string{"LOCALIZE[30203]", "XBMC.Action(Info)"},
				[]string{"LOCALIZE[30268]", "XBMC.Action(ToggleWatched)"},
				[]string{"LOCALIZE[30037]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/setviewmode/episodes"))},
			}
		} else {
			item.ContextMenu = [][]string{
				[]string{contextLabel, fmt.Sprintf("XBMC.PlayMedia(%s)", contextURL)},
				[]string{"LOCALIZE[30037]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/setviewmode/episodes"))},
			}
		}
		item.IsPlayable = true
	}

	ctx.JSON(200, xbmc.NewView("episodes", filterListItems(items)))
}

func showSeasonLinks(showID int, seasonNumber int) ([]*bittorrent.TorrentFile, error) {
	log.Info("Searching links for TMDB Id:", showID)

	show := tmdb.GetShow(showID, config.Get().Language)
	if show == nil {
		return nil, errors.New("Unable to find show")
	}

	season := tmdb.GetSeason(showID, seasonNumber, config.Get().Language)
	if season == nil {
		return nil, errors.New("Unable to find season")
	}

	log.Info("Resolved %d to %s", showID, show.Name)

	searchers := providers.GetSeasonSearchers()
	if len(searchers) == 0 {
		xbmc.Notify("Elementum", "LOCALIZE[30204]", config.AddonIcon())
	}

	return providers.SearchSeason(searchers, show, season), nil
}

// ShowSeasonLinks ...
func ShowSeasonLinks(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")

		showID, _ := strconv.Atoi(ctx.Params.ByName("showId"))
		seasonNumber, _ := strconv.Atoi(ctx.Params.ByName("season"))
		external := ctx.Query("external")

		show := tmdb.GetShow(showID, "")
		if show == nil {
			ctx.Error(errors.New("Unable to find show"))
			return
		}

		season := tmdb.GetSeason(showID, seasonNumber, config.Get().Language)
		if season == nil {
			ctx.Error(errors.New("Unable to find season"))
			return
		}

		longName := fmt.Sprintf("%s Season %02d", show.Name, seasonNumber)

		existingTorrent := s.HasTorrentBySeason(showID, seasonNumber)
		if existingTorrent != "" && (config.Get().SilentStreamStart || xbmc.DialogConfirmFocused("Elementum", "LOCALIZE[30270]")) {
			rURL := URLQuery(URLForXBMC("/play"),
				"resume", existingTorrent,
				"tmdb", strconv.Itoa(season.ID),
				"type", "episode")
			if external != "" {
				xbmc.PlayURL(rURL)
			} else {
				ctx.Redirect(302, rURL)
			}
			return
		}

		if torrent := InTorrentsMap(strconv.Itoa(season.ID)); torrent != nil {
			rURL := URLQuery(
				URLForXBMC("/play"), "uri", torrent.URI,
				"tmdb", strconv.Itoa(season.ID),
				"type", "episode")
			if external != "" {
				xbmc.PlayURL(rURL)
			} else {
				ctx.Redirect(302, rURL)
			}
			return
		}

		var torrents []*bittorrent.TorrentFile
		var err error

		fakeTmdbID := strconv.Itoa(showID) + "_" + strconv.Itoa(seasonNumber)
		if torrents, err = GetCachedTorrents(fakeTmdbID); err != nil || len(torrents) == 0 {
			torrents, err = showSeasonLinks(showID, seasonNumber)

			SetCachedTorrents(fakeTmdbID, torrents)
		}

		if err != nil {
			ctx.Error(err)
			return
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

		choice := xbmc.ListDialogLarge("LOCALIZE[30228]", longName, choices...)
		if choice >= 0 {
			AddToTorrentsMap(strconv.Itoa(season.ID), torrents[choice])

			rURL := URLQuery(URLForXBMC("/play"), "uri", torrents[choice].URI)

			if external != "" {
				xbmc.PlayURL(rURL)
			} else {
				ctx.Redirect(302, rURL)
			}
		}
	}
}

// ShowSeasonPlay ...
func ShowSeasonPlay(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")

		showID, _ := strconv.Atoi(ctx.Params.ByName("showId"))
		seasonNumber, _ := strconv.Atoi(ctx.Params.ByName("season"))
		external := ctx.Query("external")

		show := tmdb.GetShow(showID, "")
		if show == nil {
			ctx.Error(errors.New("Unable to find show"))
			return
		}

		season := tmdb.GetSeason(showID, seasonNumber, config.Get().Language)
		if season == nil {
			ctx.Error(errors.New("Unable to find season"))
			return
		}

		existingTorrent := s.HasTorrentBySeason(showID, seasonNumber)
		if existingTorrent != "" && (config.Get().SilentStreamStart || xbmc.DialogConfirmFocused("Elementum", "LOCALIZE[30270]")) {
			rURL := URLQuery(URLForXBMC("/play"),
				"resume", existingTorrent,
				"tmdb", strconv.Itoa(season.ID),
				"type", "episode")
			if external != "" {
				xbmc.PlayURL(rURL)
			} else {
				ctx.Redirect(302, rURL)
			}
			return
		}

		if torrent := InTorrentsMap(strconv.Itoa(season.ID)); torrent != nil {
			rURL := URLQuery(
				URLForXBMC("/play"), "uri", torrent.URI,
				"tmdb", strconv.Itoa(season.ID),
				"type", "episode")
			if external != "" {
				xbmc.PlayURL(rURL)
			} else {
				ctx.Redirect(302, rURL)
			}
			return
		}

		var torrents []*bittorrent.TorrentFile
		var err error

		fakeTmdbID := strconv.Itoa(showID) + "_" + strconv.Itoa(seasonNumber)
		if torrents, err = GetCachedTorrents(fakeTmdbID); err != nil || len(torrents) == 0 {
			torrents, err = showSeasonLinks(showID, seasonNumber)

			SetCachedTorrents(fakeTmdbID, torrents)
		}

		if err != nil {
			ctx.Error(err)
			return
		}

		if len(torrents) == 0 {
			xbmc.Notify("Elementum", "LOCALIZE[30205]", config.AddonIcon())
			return
		}

		if len(torrents) == 0 {
			xbmc.Notify("Elementum", "LOCALIZE[30205]", config.AddonIcon())
			return
		}

		AddToTorrentsMap(strconv.Itoa(season.ID), torrents[0])

		rURL := URLQuery(
			URLForXBMC("/play"), "uri", torrents[0].URI,
			"tmdb", strconv.Itoa(season.ID),
			"show", strconv.Itoa(showID),
			"season", ctx.Params.ByName("season"),
			"episode", ctx.Params.ByName("episode"),
			"type", "episode")
		if external != "" {
			xbmc.PlayURL(rURL)
		} else {
			ctx.Redirect(302, rURL)
		}
	}
}

func showEpisodeLinks(showID int, seasonNumber int, episodeNumber int) ([]*bittorrent.TorrentFile, error) {
	log.Info("Searching links for TMDB Id:", showID)

	show := tmdb.GetShow(showID, config.Get().Language)
	if show == nil {
		return nil, errors.New("Unable to find show")
	}

	season := tmdb.GetSeason(showID, seasonNumber, config.Get().Language)
	if season == nil || len(season.Episodes) < episodeNumber {
		return nil, errors.New("Unable to find season")
	}

	episode := season.Episodes[episodeNumber-1]

	log.Infof("Resolved %d to %s", showID, show.Name)

	searchers := providers.GetEpisodeSearchers()
	if len(searchers) == 0 {
		xbmc.Notify("Elementum", "LOCALIZE[30204]", config.AddonIcon())
	}

	return providers.SearchEpisode(searchers, show, episode), nil
}

// ShowEpisodePlaySelector ...
func ShowEpisodePlaySelector(link string, s *bittorrent.Service) gin.HandlerFunc {
	play := strings.Contains(link, "play")

	if !strings.Contains(link, "force") && config.Get().ForceLinkType {
		if config.Get().ChooseStreamAuto {
			play = true
		} else {
			play = false
		}
	}

	if play {
		return ShowEpisodePlay(s)
	}
	return ShowEpisodeLinks(s)
}

// ShowEpisodeLinks ...
func ShowEpisodeLinks(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")

		tmdbID := ctx.Params.ByName("showId")
		showID, _ := strconv.Atoi(tmdbID)
		seasonNumber, _ := strconv.Atoi(ctx.Params.ByName("season"))
		episodeNumber, _ := strconv.Atoi(ctx.Params.ByName("episode"))
		external := ctx.Query("external")
		doresume := ctx.DefaultQuery("doresume", "true")

		show := tmdb.GetShow(showID, "")
		if show == nil {
			ctx.Error(errors.New("Unable to find show"))
			return
		}

		episode := tmdb.GetEpisode(showID, seasonNumber, episodeNumber, config.Get().Language)
		if episode == nil {
			ctx.Error(errors.New("Unable to find episode"))
			return
		}

		longName := fmt.Sprintf("%s S%02dE%02d", show.Name, seasonNumber, episodeNumber)

		existingTorrent, autoStart := s.HasTorrentByEpisode(showID, seasonNumber, episodeNumber)
		if existingTorrent != "" && (autoStart || config.Get().SilentStreamStart || xbmc.DialogConfirmFocused("Elementum", "LOCALIZE[30270]")) {
			rURL := URLQuery(URLForXBMC("/play"),
				"doresume", doresume,
				"resume", existingTorrent,
				"tmdb", strconv.Itoa(episode.ID),
				"show", tmdbID,
				"season", ctx.Params.ByName("season"),
				"episode", ctx.Params.ByName("episode"),
				"type", "episode")
			if external != "" {
				xbmc.PlayURL(rURL)
			} else {
				ctx.Redirect(302, rURL)
			}
			return
		}

		if torrent := InTorrentsMap(strconv.Itoa(episode.ID)); torrent != nil {
			rURL := URLQuery(URLForXBMC("/play"),
				"doresume", doresume,
				"uri", torrent.URI,
				"tmdb", strconv.Itoa(episode.ID),
				"show", tmdbID,
				"season", ctx.Params.ByName("season"),
				"episode", ctx.Params.ByName("episode"),
				"type", "episode")
			if external != "" {
				xbmc.PlayURL(rURL)
			} else {
				ctx.Redirect(302, rURL)
			}
			return
		}

		var torrents []*bittorrent.TorrentFile
		var err error

		fakeTmdbID := strconv.Itoa(showID) + "_" + strconv.Itoa(seasonNumber) + "_" + strconv.Itoa(episodeNumber)
		if torrents, err = GetCachedTorrents(fakeTmdbID); err != nil || len(torrents) == 0 {
			torrents, err = showEpisodeLinks(showID, seasonNumber, episodeNumber)

			SetCachedTorrents(fakeTmdbID, torrents)
		}

		if err != nil {
			ctx.Error(err)
			return
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

		choice := xbmc.ListDialogLarge("LOCALIZE[30228]", longName, choices...)
		if choice >= 0 {
			AddToTorrentsMap(strconv.Itoa(episode.ID), torrents[choice])

			rURL := URLQuery(URLForXBMC("/play"),
				"doresume", doresume,
				"uri", torrents[choice].URI,
				"tmdb", strconv.Itoa(episode.ID),
				"show", tmdbID,
				"season", ctx.Params.ByName("season"),
				"episode", ctx.Params.ByName("episode"),
				"type", "episode")
			if external != "" {
				xbmc.PlayURL(rURL)
			} else {
				ctx.Redirect(302, rURL)
			}
		}
	}
}

// ShowEpisodePlay ...
func ShowEpisodePlay(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")

		tmdbID := ctx.Params.ByName("showId")
		showID, _ := strconv.Atoi(tmdbID)
		seasonNumber, _ := strconv.Atoi(ctx.Params.ByName("season"))
		episodeNumber, _ := strconv.Atoi(ctx.Params.ByName("episode"))
		external := ctx.Query("external")
		doresume := ctx.DefaultQuery("doresume", "true")

		show := tmdb.GetShow(showID, "")
		if show == nil {
			ctx.Error(errors.New("Unable to find show"))
			return
		}

		episode := tmdb.GetEpisode(showID, seasonNumber, episodeNumber, config.Get().Language)
		if episode == nil {
			ctx.Error(errors.New("Unable to find episode"))
			return
		}

		existingTorrent, autoStart := s.HasTorrentByEpisode(showID, seasonNumber, episodeNumber)
		if existingTorrent != "" && (autoStart || xbmc.DialogConfirmFocused("Elementum", "LOCALIZE[30270]")) {
			rURL := URLQuery(URLForXBMC("/play"),
				"doresume", doresume,
				"resume", existingTorrent,
				"tmdb", strconv.Itoa(episode.ID),
				"show", tmdbID,
				"season", ctx.Params.ByName("season"),
				"episode", ctx.Params.ByName("episode"),
				"type", "episode")
			if external != "" {
				xbmc.PlayURL(rURL)
			} else {
				ctx.Redirect(302, rURL)
			}
			return
		}

		if torrent := InTorrentsMap(strconv.Itoa(episode.ID)); torrent != nil {
			rURL := URLQuery(URLForXBMC("/play"),
				"doresume", doresume,
				"uri", torrent.URI,
				"tmdb", strconv.Itoa(episode.ID),
				"show", tmdbID,
				"season", ctx.Params.ByName("season"),
				"episode", ctx.Params.ByName("episode"),
				"type", "episode")
			if external != "" {
				xbmc.PlayURL(rURL)
			} else {
				ctx.Redirect(302, rURL)
			}
			return
		}

		var torrents []*bittorrent.TorrentFile
		var err error

		fakeTmdbID := strconv.Itoa(showID) + "_" + strconv.Itoa(seasonNumber) + "_" + strconv.Itoa(episodeNumber)
		if torrents, err = GetCachedTorrents(fakeTmdbID); err != nil || len(torrents) == 0 {
			torrents, err = showEpisodeLinks(showID, seasonNumber, episodeNumber)

			SetCachedTorrents(fakeTmdbID, torrents)
		}

		if len(torrents) == 0 {
			xbmc.Notify("Elementum", "LOCALIZE[30205]", config.AddonIcon())
			return
		}

		AddToTorrentsMap(strconv.Itoa(episode.ID), torrents[0])

		rURL := URLQuery(URLForXBMC("/play"),
			"doresume", doresume,
			"uri", torrents[0].URI,
			"tmdb", strconv.Itoa(episode.ID),
			"show", tmdbID,
			"season", ctx.Params.ByName("season"),
			"episode", ctx.Params.ByName("episode"),
			"type", "episode")
		if external != "" {
			xbmc.PlayURL(rURL)
		} else {
			ctx.Redirect(302, rURL)
		}
	}
}
