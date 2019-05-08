package bittorrent

const (
	movieType   = "movie"
	showType    = "show"
	episodeType = "episode"
)

const (
	// StorageFile ...
	StorageFile int = iota
	// StorageMemory ...
	StorageMemory
)

var (
	// Storages ...
	Storages = []string{
		"File",
		"Memory",
	}
)

const (
	// StatusQueued ...
	StatusQueued = iota
	// StatusChecking ...
	StatusChecking
	// StatusFinding ...
	StatusFinding
	// StatusDownloading ...
	StatusDownloading
	// StatusFinished ...
	StatusFinished
	// StatusSeeding ...
	StatusSeeding
	// StatusAllocating ...
	StatusAllocating
	// StatusStalled ...
	StatusStalled
	// StatusPaused ...
	StatusPaused
	// StatusBuffering ...
	StatusBuffering
	// StatusPlaying ...
	StatusPlaying
)

// StatusStrings ...
var StatusStrings = []string{
	"Queued",
	"Checking",
	"Finding",
	"Downloading",
	"Finished",
	"Seeding",
	"Allocating",
	"Stalled",
	"Paused",
	"Buffering",
	"Playing",
}

const (
	// Remove ...
	Remove = iota
	// Active ...
	Active
)

const (
	profileDefault = iota
	profileMinMemory
	profileHighSpeed
)

const (
	magnetEnricherAsIs = iota
	magnetEnricherClear
	magnetEnricherAdd
)

const (
	ipToSDefault     = iota
	ipToSLowDelay    = 1 << iota
	ipToSReliability = 1 << iota
	ipToSThroughput  = 1 << iota
	ipToSLowCost     = 1 << iota
)

var dhtBootstrapNodes = []string{
	"router.bittorrent.com:6881",
	"router.utorrent.com:6881",
	"dht.transmissionbt.com:6881",
	"dht.aelitis.com:6881",     // Vuze
	"dht.libtorrent.org:25401", // Libtorrent
}

// DefaultTrackers ...
var DefaultTrackers = []string{
	"http://bt4.t-ru.org/ann?magnet",
	"http://retracker.mgts.by:80/announce",
	"http://tracker.city9x.com:2710/announce",
	"http://tracker.electro-torrent.pl:80/announce",
	"http://tracker.internetwarriors.net:1337/announce",
	"http://bt.svao-ix.ru/announce",

	"udp://tracker.opentrackr.org:1337/announce",
	"udp://tracker.coppersurfer.tk:6969/announce",
	"udp://tracker.leechers-paradise.org:6969/announce",
	"udp://tracker.openbittorrent.com:80/announce",
	"udp://public.popcorn-tracker.org:6969/announce",
	"udp://explodie.org:6969",
	"udp://46.148.18.250:2710",
	"udp://opentor.org:2710",
}

const (
	ltAlertWaitTime = 1 // 1 second
)

const (
	// ProxyTypeNone ...
	ProxyTypeNone = iota
	// ProxyTypeSocks4 ...
	ProxyTypeSocks4
	// ProxyTypeSocks5 ...
	ProxyTypeSocks5
	// ProxyTypeSocks5Password ...
	ProxyTypeSocks5Password
	// ProxyTypeSocksHTTP ...
	ProxyTypeSocksHTTP
	// ProxyTypeSocksHTTPPassword ...
	ProxyTypeSocksHTTPPassword
	// ProxyTypeI2PSAM ...
	ProxyTypeI2PSAM
)

const (
	storedResumeExpiration = 30 * 60 * 60 * 24
)
