package api

import (
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

// Maps TMDB movie genre ids to slugs for images
var genreSlugs = map[int]string{
	28:    "action",
	10759: "action",
	12:    "adventure",
	16:    "animation",
	35:    "comedy",
	80:    "crime",
	99:    "documentary",
	18:    "drama",
	10761: "education",
	10751: "family",
	14:    "fantasy",
	10769: "foreign",
	36:    "history",
	27:    "horror",
	10762: "kids",
	10402: "music",
	9648:  "mystery",
	10763: "news",
	10764: "reality",
	10749: "romance",
	878:   "scifi",
	10765: "scifi",
	10766: "soap",
	10767: "talk",
	10770: "tv",
	53:    "thriller",
	10752: "war",
	10768: "war",
	37:    "western",
}

// MoviesIndex ...
func MoviesIndex(ctx *gin.Context) {
	items := xbmc.ListItems{
		{Label: "LOCALIZE[30209]", Path: URLForXBMC("/movies/search"), Thumbnail: config.AddonResource("img", "search.png")},
		{Label: "LOCALIZE[30263]", Path: URLForXBMC("/movies/trakt/lists/"), Thumbnail: config.AddonResource("img", "trakt.png"), TraktAuth: true},
		{Label: "LOCALIZE[30254]", Path: URLForXBMC("/movies/trakt/watchlist"), Thumbnail: config.AddonResource("img", "trakt.png"), ContextMenu: [][]string{[]string{"LOCALIZE[30252]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/library/movie/list/add/watchlist"))}}, TraktAuth: true},
		{Label: "LOCALIZE[30257]", Path: URLForXBMC("/movies/trakt/collection"), Thumbnail: config.AddonResource("img", "trakt.png"), ContextMenu: [][]string{[]string{"LOCALIZE[30252]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/library/movie/list/add/collection"))}}, TraktAuth: true},
		{Label: "LOCALIZE[30290]", Path: URLForXBMC("/movies/trakt/calendars/"), Thumbnail: config.AddonResource("img", "most_anticipated.png"), TraktAuth: true},
		{Label: "LOCALIZE[30423]", Path: URLForXBMC("/movies/trakt/recommendations"), Thumbnail: config.AddonResource("img", "movies.png"), TraktAuth: true},
		{Label: "LOCALIZE[30422]", Path: URLForXBMC("/movies/trakt/toplists"), Thumbnail: config.AddonResource("img", "most_collected.png")},
		{Label: "LOCALIZE[30246]", Path: URLForXBMC("/movies/trakt/trending"), Thumbnail: config.AddonResource("img", "trending.png")},
		{Label: "LOCALIZE[30210]", Path: URLForXBMC("/movies/trakt/popular"), Thumbnail: config.AddonResource("img", "popular.png")},
		{Label: "LOCALIZE[30247]", Path: URLForXBMC("/movies/trakt/played"), Thumbnail: config.AddonResource("img", "most_played.png")},
		{Label: "LOCALIZE[30248]", Path: URLForXBMC("/movies/trakt/watched"), Thumbnail: config.AddonResource("img", "most_watched.png")},
		{Label: "LOCALIZE[30249]", Path: URLForXBMC("/movies/trakt/collected"), Thumbnail: config.AddonResource("img", "most_collected.png")},
		{Label: "LOCALIZE[30250]", Path: URLForXBMC("/movies/trakt/anticipated"), Thumbnail: config.AddonResource("img", "most_anticipated.png")},
		{Label: "LOCALIZE[30251]", Path: URLForXBMC("/movies/trakt/boxoffice"), Thumbnail: config.AddonResource("img", "box_office.png")},

		{Label: "LOCALIZE[30210]", Path: URLForXBMC("/movies/popular"), Thumbnail: config.AddonResource("img", "popular.png")},
		{Label: "LOCALIZE[30211]", Path: URLForXBMC("/movies/top"), Thumbnail: config.AddonResource("img", "top_rated.png")},
		{Label: "LOCALIZE[30212]", Path: URLForXBMC("/movies/mostvoted"), Thumbnail: config.AddonResource("img", "most_voted.png")},
		{Label: "LOCALIZE[30236]", Path: URLForXBMC("/movies/recent"), Thumbnail: config.AddonResource("img", "clock.png")},
		{Label: "LOCALIZE[30213]", Path: URLForXBMC("/movies/imdb250"), Thumbnail: config.AddonResource("img", "imdb.png")},
		{Label: "LOCALIZE[30289]", Path: URLForXBMC("/movies/genres"), Thumbnail: config.AddonResource("img", "genre_comedy.png")},
		{Label: "LOCALIZE[30373]", Path: URLForXBMC("/movies/languages"), Thumbnail: config.AddonResource("img", "movies.png")},
		{Label: "LOCALIZE[30374]", Path: URLForXBMC("/movies/countries"), Thumbnail: config.AddonResource("img", "movies.png")},

		{Label: "LOCALIZE[30361]", Path: URLForXBMC("/movies/trakt/history"), Thumbnail: config.AddonResource("img", "trakt.png"), TraktAuth: true},

		{Label: "LOCALIZE[30517]", Path: URLForXBMC("/movies/library"), Thumbnail: config.AddonResource("img", "movies.png")},
	}
	for _, item := range items {
		item.ContextMenu = [][]string{
			[]string{"LOCALIZE[30142]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/setviewmode/menus_movies"))},
		}
	}

	// Adding items from custom menu
	if MovieMenu.AddItems != nil && len(MovieMenu.AddItems) > 0 {
		index := 1
		for _, i := range MovieMenu.AddItems {
			item := &xbmc.ListItem{Label: i.Name, Path: i.Link, Thumbnail: config.AddonResource("img", "movies.png")}
			item.ContextMenu = [][]string{
				[]string{"LOCALIZE[30521]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLQuery(URLForXBMC("/menu/movie/remove"), "name", i.Name, "link", i.Link))},
			}

			items = append(items[:index], append([]*xbmc.ListItem{item}, items[index:]...)...)
			index++
		}
	}

	ctx.JSON(200, xbmc.NewView("menus_movies", filterListItems(items)))
}

// MovieGenres ...
func MovieGenres(ctx *gin.Context) {
	items := make(xbmc.ListItems, 0)
	for _, genre := range tmdb.GetMovieGenres(config.Get().Language) {
		slug, _ := genreSlugs[genre.ID]
		items = append(items, &xbmc.ListItem{
			Label:     genre.Name,
			Path:      URLForXBMC("/movies/popular/genre/%s", strconv.Itoa(genre.ID)),
			Thumbnail: config.AddonResource("img", fmt.Sprintf("genre_%s.png", slug)),
			ContextMenu: [][]string{
				[]string{"LOCALIZE[30236]", fmt.Sprintf("Container.Update(%s)", URLForXBMC("/movies/recent/genre/%s", strconv.Itoa(genre.ID)))},
				[]string{"LOCALIZE[30144]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/setviewmode/menus_movies_genres"))},
			},
		})
	}
	ctx.JSON(200, xbmc.NewView("menus_movies_genres", filterListItems(items)))
}

// MovieLanguages ...
func MovieLanguages(ctx *gin.Context) {
	items := make(xbmc.ListItems, 0)
	for _, language := range tmdb.GetLanguages(config.Get().Language) {
		items = append(items, &xbmc.ListItem{
			Label: language.Name,
			Path:  URLForXBMC("/movies/popular/language/%s", language.Iso639_1),
			ContextMenu: [][]string{
				[]string{"LOCALIZE[30236]", fmt.Sprintf("Container.Update(%s)", URLForXBMC("/movies/recent/language/%s", language.Iso639_1))},
				[]string{"LOCALIZE[30144]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/setviewmode/menus_movies_languages"))},
			},
		})
	}
	ctx.JSON(200, xbmc.NewView("menus_movies_languages", filterListItems(items)))
}

// MovieCountries ...
func MovieCountries(ctx *gin.Context) {
	items := make(xbmc.ListItems, 0)
	for _, country := range tmdb.GetCountries(config.Get().Language) {
		items = append(items, &xbmc.ListItem{
			Label: country.EnglishName,
			Path:  URLForXBMC("/movies/popular/country/%s", country.Iso31661),
			ContextMenu: [][]string{
				[]string{"LOCALIZE[30236]", fmt.Sprintf("Container.Update(%s)", URLForXBMC("/movies/recent/country/%s", country.Iso31661))},
				[]string{"LOCALIZE[30144]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/setviewmode/menus_movies_countries"))},
			},
		})
	}
	ctx.JSON(200, xbmc.NewView("menus_movies_countries", filterListItems(items)))
}

// MovieLibrary ...
func MovieLibrary(ctx *gin.Context) {
	movies, err := xbmc.VideoLibraryGetElementumMovies()
	if err != nil || movies == nil || movies.Limits == nil || movies.Limits.Total == 0 {
		return
	}

	tmdbMovies := tmdb.Movies{}
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))

	for i := (page - 1) * config.Get().ResultsPerPage; i < movies.Limits.Total && i < page*config.Get().ResultsPerPage; i++ {
		if movies == nil || movies.Movies == nil || len(movies.Movies) < i {
			continue
		}

		if id, err := strconv.Atoi(movies.Movies[i].UniqueIDs.Elementum); err == nil {
			m := tmdb.GetMovie(id, config.Get().Language)
			if m != nil {
				tmdbMovies = append(tmdbMovies, m)
			}
		}
	}

	renderMovies(ctx, tmdbMovies, page, movies.Limits.Total, "")
}

// TopTraktLists ...
func TopTraktLists(ctx *gin.Context) {
	pageParam := ctx.DefaultQuery("page", "1")
	page, _ := strconv.Atoi(pageParam)

	items := xbmc.ListItems{}
	lists, hasNextPage := trakt.TopLists(pageParam)
	for _, list := range lists {
		link := URLForXBMC("/movies/trakt/lists/%s/%d", list.List.User.Ids.Slug, list.List.IDs.Trakt)
		menuItem := []string{"LOCALIZE[30520]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLQuery(URLForXBMC("/menu/movie/add"), "name", list.List.Name, "link", link))}
		if MovieMenu.Contains(addAction, &MenuItem{Name: list.List.Name, Link: link}) {
			menuItem = []string{"LOCALIZE[30521]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLQuery(URLForXBMC("/menu/movie/remove"), "name", list.List.Name, "link", link))}
		}

		item := &xbmc.ListItem{
			Label:     list.List.Name,
			Path:      link,
			Thumbnail: config.AddonResource("img", "trakt.png"),
			ContextMenu: [][]string{
				menuItem,
			},
		}
		items = append(items, item)
	}
	if hasNextPage {
		path := ctx.Request.URL.Path
		nextpage := &xbmc.ListItem{
			Label:     "LOCALIZE[30415];;" + strconv.Itoa(page+1),
			Path:      URLForXBMC(fmt.Sprintf("%s?page=%d", path, page+1)),
			Thumbnail: config.AddonResource("img", "nextpage.png"),
		}
		items = append(items, nextpage)
	}

	ctx.JSON(200, xbmc.NewView("menus_movies", filterListItems(items)))
}

// MoviesTraktLists ...
func MoviesTraktLists(ctx *gin.Context) {
	items := xbmc.ListItems{}
	lists := trakt.Userlists()
	lists = append(lists, trakt.Likedlists()...)

	sort.Slice(lists, func(i int, j int) bool {
		return lists[i].Name < lists[j].Name
	})

	for _, list := range lists {
		link := URLForXBMC("/movies/trakt/lists/%s/%d", list.User.Ids.Slug, list.IDs.Trakt)
		menuItem := []string{"LOCALIZE[30520]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLQuery(URLForXBMC("/menu/movie/add"), "name", list.Name, "link", link))}
		if MovieMenu.Contains(addAction, &MenuItem{Name: list.Name, Link: link}) {
			menuItem = []string{"LOCALIZE[30521]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLQuery(URLForXBMC("/menu/movie/remove"), "name", list.Name, "link", link))}
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
	ctx.JSON(200, xbmc.NewView("menus_movies", filterListItems(items)))
}

// CalendarMovies ...
func CalendarMovies(ctx *gin.Context) {
	items := xbmc.ListItems{
		{Label: "LOCALIZE[30291]", Path: URLForXBMC("/movies/trakt/calendars/movies"), Thumbnail: config.AddonResource("img", "box_office.png")},
		{Label: "LOCALIZE[30292]", Path: URLForXBMC("/movies/trakt/calendars/releases"), Thumbnail: config.AddonResource("img", "tv.png")},
		{Label: "LOCALIZE[30293]", Path: URLForXBMC("/movies/trakt/calendars/allmovies"), Thumbnail: config.AddonResource("img", "box_office.png")},
		{Label: "LOCALIZE[30294]", Path: URLForXBMC("/movies/trakt/calendars/allreleases"), Thumbnail: config.AddonResource("img", "tv.png")},
	}
	ctx.JSON(200, xbmc.NewView("menus_movies", filterListItems(items)))
}

func renderMovies(ctx *gin.Context, movies tmdb.Movies, page int, total int, query string) {
	hasNextPage := 0
	if page > 0 {
		if page*config.Get().ResultsPerPage < total {
			hasNextPage = 1
		}
	}

	items := make(xbmc.ListItems, 0, len(movies)+hasNextPage)

	for _, movie := range movies {
		if movie == nil {
			continue
		}
		item := movie.ToListItem()

		thisURL := URLForXBMC("/movie/%d/", movie.ID) + "%s/%s"
		contextLabel := playLabel
		contextTitle := fmt.Sprintf("%s (%d)", item.Info.OriginalTitle, item.Info.Year)
		contextURL := contextPlayOppositeURL(thisURL, contextTitle, false)
		if config.Get().ChooseStreamAuto {
			contextLabel = linksLabel
		}

		item.Path = contextPlayURL(thisURL, contextTitle, false)

		tmdbID := strconv.Itoa(movie.ID)

		libraryActions := [][]string{
			[]string{contextLabel, fmt.Sprintf("XBMC.PlayMedia(%s)", contextURL)},
		}
		if err := library.IsDuplicateMovie(tmdbID); err != nil || library.IsAddedToLibrary(tmdbID, library.MovieType) {
			libraryActions = append(libraryActions, []string{"LOCALIZE[30283]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/library/movie/add/%d?force=true", movie.ID))})
			libraryActions = append(libraryActions, []string{"LOCALIZE[30253]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/library/movie/remove/%d", movie.ID))})
		} else {
			libraryActions = append(libraryActions, []string{"LOCALIZE[30252]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/library/movie/add/%d", movie.ID))})
		}

		watchlistAction := []string{"LOCALIZE[30255]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/movie/%d/watchlist/add", movie.ID))}
		if inMoviesWatchlist(movie.ID) {
			watchlistAction = []string{"LOCALIZE[30256]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/movie/%d/watchlist/remove", movie.ID))}
		}

		collectionAction := []string{"LOCALIZE[30258]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/movie/%d/collection/add", movie.ID))}
		if inMoviesCollection(movie.ID) {
			collectionAction = []string{"LOCALIZE[30259]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/movie/%d/collection/remove", movie.ID))}
		}

		item.ContextMenu = [][]string{
			watchlistAction,
			collectionAction,
			[]string{"LOCALIZE[30034]", fmt.Sprintf("XBMC.RunPlugin(%s)", URLForXBMC("/setviewmode/movies"))},
		}
		item.ContextMenu = append(libraryActions, item.ContextMenu...)

		if config.Get().Platform.Kodi < 17 {
			item.ContextMenu = append(item.ContextMenu,
				[]string{"LOCALIZE[30203]", "XBMC.Action(Info)"},
				[]string{"LOCALIZE[30268]", "XBMC.Action(ToggleWatched)"},
			)
		}

		item.IsPlayable = true
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
	ctx.JSON(200, xbmc.NewView("movies", filterListItems(items)))
}

// PopularMovies ...
func PopularMovies(ctx *gin.Context) {
	p := tmdb.DiscoverFilters{}
	p.Genre = ctx.Params.ByName("genre")
	p.Language = ctx.Params.ByName("language")
	p.Country = ctx.Params.ByName("country")
	if p.Genre == "0" {
		p.Genre = ""
	}

	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	movies, total := tmdb.PopularMovies(p, config.Get().Language, page)
	renderMovies(ctx, movies, page, total, "")
}

// RecentMovies ...
func RecentMovies(ctx *gin.Context) {
	p := tmdb.DiscoverFilters{}
	p.Genre = ctx.Params.ByName("genre")
	p.Language = ctx.Params.ByName("language")
	p.Country = ctx.Params.ByName("country")
	if p.Genre == "0" {
		p.Genre = ""
	}

	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	movies, total := tmdb.RecentMovies(p, config.Get().Language, page)
	renderMovies(ctx, movies, page, total, "")
}

// TopRatedMovies ...
func TopRatedMovies(ctx *gin.Context) {
	genre := ctx.Params.ByName("genre")
	if genre == "0" {
		genre = ""
	}
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	movies, total := tmdb.TopRatedMovies(genre, config.Get().Language, page)
	renderMovies(ctx, movies, page, total, "")
}

// IMDBTop250 ...
func IMDBTop250(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	movies, total := tmdb.GetIMDBList("522effe419c2955e9922fcf3", config.Get().Language, page)
	renderMovies(ctx, movies, page, total, "")
}

// MoviesMostVoted ...
func MoviesMostVoted(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	movies, total := tmdb.MostVotedMovies("", config.Get().Language, page)
	renderMovies(ctx, movies, page, total, "")
}

// SearchMovies ...
func SearchMovies(ctx *gin.Context) {
	ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
	query := ctx.Query("q")
	keyboard := ctx.Query("keyboard")

	if len(query) == 0 {
		historyType := "movies"
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
	movies, total := tmdb.SearchMovies(query, config.Get().Language, page)
	renderMovies(ctx, movies, page, total, query)
}

func movieLinks(tmdbID string) []*bittorrent.TorrentFile {
	log.Info("Searching links for:", tmdbID)

	movie := tmdb.GetMovieByID(tmdbID, config.Get().Language)

	log.Infof("Resolved %s to %s", tmdbID, movie.Title)

	searchers := providers.GetMovieSearchers()
	if len(searchers) == 0 {
		xbmc.Notify("Elementum", "LOCALIZE[30204]", config.AddonIcon())
	}

	return providers.SearchMovie(searchers, movie)
}

// MoviePlaySelector ...
func MoviePlaySelector(link string, s *bittorrent.Service) gin.HandlerFunc {
	play := strings.Contains(link, "play")

	if !strings.Contains(link, "force") && config.Get().ForceLinkType {
		if config.Get().ChooseStreamAuto {
			play = true
		} else {
			play = false
		}
	}

	if play {
		return MoviePlay(s)
	}
	return MovieLinks(s)
}

// MovieLinks ...
func MovieLinks(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")

		tmdbID := ctx.Params.ByName("tmdbId")
		external := ctx.Query("external")
		doresume := ctx.DefaultQuery("doresume", "true")

		movie := tmdb.GetMovieByID(tmdbID, config.Get().Language)
		if movie == nil {
			return
		}

		existingTorrent := s.HasTorrentByID(movie.ID)
		if existingTorrent != "" && (config.Get().SilentStreamStart || xbmc.DialogConfirmFocused("Elementum", "LOCALIZE[30270]")) {
			rURL := URLQuery(URLForXBMC("/play"),
				"doresume", doresume,
				"resume", existingTorrent,
				"tmdb", tmdbID,
				"type", "movie")
			if external != "" {
				xbmc.PlayURL(rURL)
			} else {
				ctx.Redirect(302, rURL)
			}
			return
		}

		if torrent := InTorrentsMap(tmdbID); torrent != nil {
			rURL := URLQuery(URLForXBMC("/play"),
				"doresume", doresume,
				"uri", torrent.URI,
				"tmdb", tmdbID,
				"type", "movie")
			if external != "" {
				xbmc.PlayURL(rURL)
			} else {
				ctx.Redirect(302, rURL)
			}
			return
		}

		var torrents []*bittorrent.TorrentFile
		var err error

		if torrents, err = GetCachedTorrents(tmdbID); err != nil || len(torrents) == 0 {
			torrents = movieLinks(tmdbID)

			SetCachedTorrents(tmdbID, torrents)
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

		choice := xbmc.ListDialogLarge("LOCALIZE[30228]", movie.Title, choices...)
		if choice >= 0 {
			AddToTorrentsMap(tmdbID, torrents[choice])

			rURL := URLQuery(URLForXBMC("/play"),
				"uri", torrents[choice].URI,
				"doresume", doresume,
				"tmdb", tmdbID,
				"type", "movie")
			if external != "" {
				xbmc.PlayURL(rURL)
			} else {
				ctx.Redirect(302, rURL)
			}
		}
	}
}

// MoviePlay ...
func MoviePlay(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")

		tmdbID := ctx.Params.ByName("tmdbId")
		external := ctx.Query("external")
		doresume := ctx.DefaultQuery("doresume", "true")

		movie := tmdb.GetMovieByID(tmdbID, config.Get().Language)
		if movie == nil {
			return
		}

		existingTorrent := s.HasTorrentByID(movie.ID)
		if existingTorrent != "" && (config.Get().SilentStreamStart || xbmc.DialogConfirmFocused("Elementum", "LOCALIZE[30270]")) {
			rURL := URLQuery(URLForXBMC("/play"),
				"doresume", doresume,
				"resume", existingTorrent,
				"tmdb", tmdbID,
				"type", "movie")
			if external != "" {
				xbmc.PlayURL(rURL)
			} else {
				ctx.Redirect(302, rURL)
			}
			return
		}

		if torrent := InTorrentsMap(tmdbID); torrent != nil {
			rURL := URLQuery(URLForXBMC("/play"),
				"doresume", doresume,
				"uri", torrent.URI,
				"tmdb", tmdbID,
				"type", "movie")
			if external != "" {
				xbmc.PlayURL(rURL)
			} else {
				ctx.Redirect(302, rURL)
			}
			return
		}

		var torrents []*bittorrent.TorrentFile
		var err error

		if torrents, err = GetCachedTorrents(tmdbID); err != nil || len(torrents) == 0 {
			torrents = movieLinks(tmdbID)

			SetCachedTorrents(tmdbID, torrents)
		}

		if len(torrents) == 0 {
			xbmc.Notify("Elementum", "LOCALIZE[30205]", config.AddonIcon())
			return
		}

		sort.Sort(sort.Reverse(providers.ByQuality(torrents)))

		AddToTorrentsMap(tmdbID, torrents[0])

		rURL := URLQuery(URLForXBMC("/play"),
			"uri", torrents[0].URI,
			"doresume", doresume,
			"tmdb", tmdbID,
			"type", "movie")
		if external != "" {
			xbmc.PlayURL(rURL)
		} else {
			ctx.Redirect(302, rURL)
		}
	}
}
