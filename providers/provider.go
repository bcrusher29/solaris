package providers

import (
	"github.com/bcrusher29/solaris/bittorrent"
	"github.com/bcrusher29/solaris/tmdb"
)

// Searcher ...
type Searcher interface {
	SearchLinks(query string) []*bittorrent.TorrentFile
}

// MovieSearcher ...
type MovieSearcher interface {
	SearchMovieLinks(movie *tmdb.Movie) []*bittorrent.TorrentFile
}

// SeasonSearcher ...
type SeasonSearcher interface {
	SearchSeasonLinks(show *tmdb.Show, season *tmdb.Season) []*bittorrent.TorrentFile
}

// EpisodeSearcher ...
type EpisodeSearcher interface {
	SearchEpisodeLinks(show *tmdb.Show, episode *tmdb.Episode) []*bittorrent.TorrentFile
}
