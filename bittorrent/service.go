package bittorrent

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cespare/xxhash"
	"github.com/dustin/go-humanize"
	"github.com/radovskyb/watcher"
	"github.com/shirou/gopsutil/mem"
	"github.com/zeebo/bencode"

	lt "github.com/ElementumOrg/libtorrent-go"

	"github.com/bcrusher29/solaris/broadcast"
	"github.com/bcrusher29/solaris/config"
	"github.com/bcrusher29/solaris/database"
	"github.com/bcrusher29/solaris/diskusage"
	"github.com/bcrusher29/solaris/scrape"
	"github.com/bcrusher29/solaris/tmdb"
	"github.com/bcrusher29/solaris/util"
	"github.com/bcrusher29/solaris/xbmc"
)

// Service ...
type Service struct {
	config *config.Configuration
	q      *Queue
	mu     sync.Mutex

	Session      lt.Session
	PackSettings lt.SettingsPack

	mappedPorts map[string]int

	InternalProxy *http.Server

	Players      map[string]*Player
	SpaceChecked map[string]bool

	UserAgent   string
	PeerID      string
	ListenIP    string
	ListenIPv6  string
	ListenPort  int
	DisableIPv6 bool

	dialogProgressBG *xbmc.DialogProgressBG

	MarkedToMove string

	alertsBroadcaster *broadcast.Broadcaster
	Closer            util.Event
	isShutdown        bool
}

type activeTorrent struct {
	torrentName  string
	downloadRate float64
	uploadRate   float64
	progress     int
}

// NewService ...
func NewService() *Service {
	now := time.Now()
	defer func() {
		log.Infof("Service started in %s", time.Since(now))
	}()

	s := &Service{
		config: config.Get(),

		SpaceChecked: map[string]bool{},
		Players:      map[string]*Player{},

		alertsBroadcaster: broadcast.NewBroadcaster(),
	}

	s.q = NewQueue(s)

	s.configure()

	go s.alertsConsumer()
	go s.logAlerts()

	go s.startServices()

	go s.watchConfig()
	go s.saveResumeDataConsumer()
	if !s.IsMemoryStorage() {
		go s.saveResumeDataLoop()
	}

	go tmdb.CheckAPIKey()

	go s.loadTorrentFiles()
	go s.downloadProgress()

	return s
}

// Close ...
func (s *Service) Close(isShutdown bool) {
	now := time.Now()

	s.isShutdown = isShutdown
	s.Closer.Set()

	log.Info("Stopping BT Services...")
	s.stopServices()

	s.CloseSession()

	log.Infof("Closed service in %s", time.Since(now))
}

// CloseSession tries to close libtorrent session with a timeout,
// because it takes too much to close and Kodi hangs.
func (s *Service) CloseSession() {
	log.Info("Closing Session")
	lt.DeleteSession(s.Session)
}

// Reconfigure fired every time addon configuration has changed
// and Kodi sent a notification about that.
// Should reassemble Service configuration and restart everything.
// For non-memory storage it should also load old torrent files.
func (s *Service) Reconfigure() {
	s.stopServices()

	config.Reload()
	scrape.Reload()

	s.config = config.Get()
	s.configure()

	if config.Get().AntizapretEnabled {
		go scrape.PacParser.Update()
	}

	s.startServices()
	s.loadTorrentFiles()
}

func (s *Service) configure() {
	log.Info("Configuring client...")

	if s.config.InternalProxyEnabled {
		log.Infof("Starting internal proxy")
		s.InternalProxy = scrape.StartProxy()
	}

	if _, err := os.Stat(s.config.TorrentsPath); os.IsNotExist(err) {
		if err := os.Mkdir(s.config.TorrentsPath, 0755); err != nil {
			log.Error("Unable to create Torrents folder")
		}
	}

	settings := lt.NewSettingsPack()

	log.Info("Applying session settings...")

	s.PeerID, s.UserAgent = util.GetUserAndPeer()
	log.Infof("UserAgent: %s, PeerID: %s", s.UserAgent, s.PeerID)
	settings.SetStr("user_agent", s.UserAgent)

	// Bools
	settings.SetBool("announce_to_all_tiers", true)
	settings.SetBool("announce_to_all_trackers", false)
	settings.SetBool("apply_ip_filter_to_trackers", false)
	settings.SetBool("lazy_bitfields", true)
	settings.SetBool("no_atime_storage", true)
	settings.SetBool("no_connect_privileged_ports", false)
	settings.SetBool("prioritize_partial_pieces", false)
	settings.SetBool("rate_limit_ip_overhead", false)
	settings.SetBool("smooth_connects", false)
	settings.SetBool("strict_end_game_mode", true)
	settings.SetBool("upnp_ignore_nonrouters", true)
	settings.SetBool("use_dht_as_fallback", false)
	settings.SetBool("use_parole_mode", true)

	// Disabling services, as they are enabled by default in libtorrent
	settings.SetBool("enable_upnp", false)
	settings.SetBool("enable_natpmp", false)
	settings.SetBool("enable_lsd", false)
	settings.SetBool("enable_dht", false)

	// settings.SetInt("peer_tos", ipToSLowCost)
	// settings.SetInt("torrent_connect_boost", 20)
	// settings.SetInt("torrent_connect_boost", 100)
	settings.SetInt("torrent_connect_boost", 0)
	settings.SetInt("aio_threads", 4)
	settings.SetInt("cache_size", -1)
	settings.SetInt("mixed_mode_algorithm", int(lt.SettingsPackPreferTcp))

	// Intervals and Timeouts
	settings.SetInt("auto_scrape_interval", 1200)
	settings.SetInt("auto_scrape_min_interval", 900)
	settings.SetInt("min_announce_interval", 30)
	settings.SetInt("dht_announce_interval", 60)
	// settings.SetInt("peer_connect_timeout", 5)
	// settings.SetInt("request_timeout", 2)
	settings.SetInt("stop_tracker_timeout", 1)

	// Ratios
	settings.SetInt("seed_time_limit", 0)
	settings.SetInt("seed_time_ratio_limit", 0)
	settings.SetInt("share_ratio_limit", 0)

	// Algorithms
	settings.SetInt("choking_algorithm", int(lt.SettingsPackFixedSlotsChoker))
	settings.SetInt("seed_choking_algorithm", int(lt.SettingsPackFastestUpload))

	// Sizes
	settings.SetInt("max_out_request_queue", 30000)
	settings.SetInt("max_allowed_in_request_queue", 20000)
	// settings.SetInt("listen_queue_size", 2000)
	// settings.SetInt("unchoke_slots_limit", 20)
	settings.SetInt("max_peerlist_size", 50000)
	settings.SetInt("dht_upload_rate_limit", 50000)
	settings.SetInt("max_pex_peers", 200)
	settings.SetInt("max_suggest_pieces", 50)
	// settings.SetInt("aio_threads", 8)

	settings.SetInt("send_buffer_low_watermark", 10*1024)
	settings.SetInt("send_buffer_watermark", 500*1024)
	settings.SetInt("send_buffer_watermark_factor", 50)

	settings.SetInt("download_rate_limit", 0)
	settings.SetInt("upload_rate_limit", 0)

	// For Android external storage / OS-mounted NAS setups
	if s.config.TunedStorage {
		settings.SetBool("use_read_cache", true)
		settings.SetBool("coalesce_reads", true)
		settings.SetBool("coalesce_writes", true)
		settings.SetInt("max_queued_disk_bytes", 10*1024*1024)
	}

	if s.config.ConnectionsLimit > 0 {
		settings.SetInt("connections_limit", s.config.ConnectionsLimit)
	} else {
		settings.SetInt("connections_limit", getPlatformSpecificConnectionLimit())
	}

	if s.config.ConnTrackerLimitAuto || s.config.ConnTrackerLimit == 0 {
		settings.SetInt("connection_speed", 100)
	} else {
		settings.SetInt("connection_speed", s.config.ConnTrackerLimit)
	}

	if s.config.LimitAfterBuffering == false {
		if s.config.DownloadRateLimit > 0 {
			log.Infof("Rate limiting download to %s", humanize.Bytes(uint64(s.config.DownloadRateLimit)))
			settings.SetInt("download_rate_limit", s.config.DownloadRateLimit)
		}
		if s.config.UploadRateLimit > 0 {
			log.Infof("Rate limiting upload to %s", humanize.Bytes(uint64(s.config.UploadRateLimit)))
			// If we have an upload rate, use the nicer bittyrant choker
			settings.SetInt("upload_rate_limit", s.config.UploadRateLimit)
			settings.SetInt("choking_algorithm", int(lt.SettingsPackBittyrantChoker))
		}
	}

	// TODO: Enable when it's working!
	// if s.config.DisableUpload {
	// 	s.Session.AddUploadExtension()
	// }

	if !s.IsMemoryStorage() && s.config.ShareRatioLimit > 0 {
		settings.SetInt("share_ratio_limit", s.config.ShareRatioLimit)
	}
	if !s.IsMemoryStorage() && s.config.SeedTimeRatioLimit > 0 {
		settings.SetInt("seed_time_ratio_limit", s.config.SeedTimeRatioLimit)
	}
	if !s.IsMemoryStorage() && s.config.SeedTimeLimit > 0 {
		settings.SetInt("seed_time_limit", s.config.SeedTimeLimit)
	}

	log.Info("Applying encryption settings...")
	settings.SetInt("allowed_enc_level", int(lt.SettingsPackPeRc4))
	settings.SetBool("prefer_rc4", true)

	if s.config.EncryptionPolicy > 0 {
		policy := int(lt.SettingsPackPeDisabled)
		level := int(lt.SettingsPackPeBoth)
		preferRc4 := false

		if s.config.EncryptionPolicy == 2 {
			policy = int(lt.SettingsPackPeForced)
			level = int(lt.SettingsPackPeRc4)
			preferRc4 = true
		}

		settings.SetInt("out_enc_policy", policy)
		settings.SetInt("in_enc_policy", policy)
		settings.SetInt("allowed_enc_level", level)
		settings.SetBool("prefer_rc4", preferRc4)
	}

	settings.SetInt("proxy_type", ProxyTypeNone)
	if s.config.ProxyEnabled && s.config.ProxyHost != "" {
		log.Info("Applying proxy settings...")
		if s.config.ProxyType == 0 {
			settings.SetInt("proxy_type", ProxyTypeSocks4)
		} else if s.config.ProxyType == 1 {
			settings.SetInt("proxy_type", ProxyTypeSocks5)
			if s.config.ProxyLogin != "" || s.config.ProxyPassword != "" {
				settings.SetInt("proxy_type", ProxyTypeSocks5Password)
			}
		} else if s.config.ProxyType == 2 {
			settings.SetInt("proxy_type", ProxyTypeSocksHTTP)
			if s.config.ProxyLogin != "" || s.config.ProxyPassword != "" {
				settings.SetInt("proxy_type", ProxyTypeSocksHTTPPassword)
			}
		} else if s.config.ProxyType == 3 {
			settings.SetInt("proxy_type", ProxyTypeI2PSAM)
			settings.SetInt("i2p_port", s.config.ProxyPort)
			settings.SetStr("i2p_hostname", s.config.ProxyHost)
			settings.SetBool("allows_i2p_mixed", true)
		}

		settings.SetInt("proxy_port", s.config.ProxyPort)
		settings.SetStr("proxy_hostname", s.config.ProxyHost)
		settings.SetStr("proxy_username", s.config.ProxyLogin)
		settings.SetStr("proxy_password", s.config.ProxyPassword)

		// Proxy files downloads
		settings.SetBool("proxy_peer_connections", config.Get().ProxyUseDownload)
		settings.SetBool("proxy_hostnames", config.Get().ProxyUseDownload)

		// Proxy Tracker connections
		settings.SetBool("proxy_tracker_connections", config.Get().ProxyUseTracker)
	}

	// Set alert_mask here so it also applies on reconfigure...
	settings.SetInt("alert_mask", int(
		lt.AlertStatusNotification|
			lt.AlertStorageNotification|
			lt.AlertErrorNotification|
			lt.AlertPerformanceWarning))

	if s.config.UseLibtorrentLogging {
		settings.SetInt("alert_mask", int(lt.AlertAllCategories))
		settings.SetInt("alert_queue_size", 2500)
	}

	log.Infof("DownloadStorage: %s", Storages[s.config.DownloadStorage])
	if s.IsMemoryStorage() {
		needSize := s.config.BufferSize + int(EndBufferSize) + 8*1024*1024

		if config.Get().MemorySize < needSize {
			log.Noticef("Raising memory size (%d) to fit all the buffer (%d)", config.Get().MemorySize, needSize)
			config.Get().MemorySize = needSize
		}

		// Set Memory storage specific settings
		settings.SetBool("close_redundant_connections", false)

		settings.SetInt("share_ratio_limit", 0)
		settings.SetInt("seed_time_ratio_limit", 0)
		settings.SetInt("seed_time_limit", 0)

		settings.SetInt("active_downloads", -1)
		settings.SetInt("active_seeds", -1)
		settings.SetInt("active_limit", -1)
		settings.SetInt("active_tracker_limit", -1)
		settings.SetInt("active_dht_limit", -1)
		settings.SetInt("active_lsd_limit", -1)
		// settings.SetInt("read_cache_line_size", 0)
		// settings.SetInt("unchoke_slots_limit", 0)

		// settings.SetInt("request_timeout", 10)
		// settings.SetInt("peer_connect_timeout", 10)

		settings.SetInt("max_out_request_queue", 50000)
		settings.SetInt("max_allowed_in_request_queue", 50000)

		// settings.SetInt("initial_picker_threshold", 20)
		// settings.SetInt("share_mode_target", 1)
		settings.SetBool("use_read_cache", false)
		settings.SetBool("auto_sequential", false)

		// settings.SetInt("tick_interval", 300)
		// settings.SetBool("strict_end_game_mode", false)

		// settings.SetInt("disk_io_write_mode", 2)
		// settings.SetInt("disk_io_read_mode", 2)
		settings.SetInt("cache_size", 0)

		// Adjust timeouts to avoid disconnect due to idling connections
		settings.SetInt("inactivity_timeout", 60*20)
		settings.SetInt("peer_timeout", 60*10)
	}

	var listenPorts []string
	if s.config.ListenAutoDetectPort {
		s.config.ListenPortMin = 6891
		s.config.ListenPortMax = 6899
	}

	for p := s.config.ListenPortMin; p <= s.config.ListenPortMax; p++ {
		listenPorts = append(listenPorts, strconv.Itoa(p))
	}
	rand.Seed(time.Now().UTC().UnixNano())

	listenInterfaces := []string{"0.0.0.0"}
	if !s.config.ListenAutoDetectIP && strings.TrimSpace(s.config.ListenInterfaces) != "" {
		listenInterfaces = strings.Split(strings.Replace(strings.TrimSpace(s.config.ListenInterfaces), " ", "", -1), ",")
	}

	s.mappedPorts = map[string]int{}
	listenInterfacesStrings := make([]string, 0)
	for _, listenInterface := range listenInterfaces {
		port := listenPorts[rand.Intn(len(listenPorts))]
		s.mappedPorts[port] = -1
		listenInterfacesStrings = append(listenInterfacesStrings, listenInterface+":"+port)
		if len(listenPorts) > 1 {
			port := listenPorts[rand.Intn(len(listenPorts))]
			s.mappedPorts[port] = -1
			listenInterfacesStrings = append(listenInterfacesStrings, listenInterface+":"+port)
		}
	}
	settings.SetStr("listen_interfaces", strings.Join(listenInterfacesStrings, ","))
	log.Infof("Listening on: %s", strings.Join(listenInterfacesStrings, ","))

	if strings.TrimSpace(s.config.OutgoingInterfaces) != "" {
		settings.SetStr("outgoing_interfaces", strings.Replace(strings.TrimSpace(s.config.OutgoingInterfaces), " ", "", -1))
	}

	if config.Get().LibtorrentProfile == profileMinMemory {
		log.Info("Setting Libtorrent profile settings to MinimalMemory")
		lt.MinMemoryUsage(settings)
	} else if config.Get().LibtorrentProfile == profileHighSpeed {
		log.Info("Setting Libtorrent profile settings to HighSpeed")
		lt.HighPerformanceSeed(settings)
	}

	s.PackSettings = settings
	s.Session = lt.NewSession(s.PackSettings, int(lt.SessionHandleAddDefaultPlugins))

	// s.Session.GetHandle().ApplySettings(s.PackSettings)

	if !s.config.LimitAfterBuffering {
		s.RestoreLimits()
	}

	s.applyCustomSettings()
}

func (s *Service) startServices() {
	log.Info("Starting LSD...")
	s.PackSettings.SetBool("enable_lsd", true)

	if s.config.DisableDHT == false {
		log.Info("Starting DHT...")
		s.PackSettings.SetStr("dht_bootstrap_nodes", strings.Join(dhtBootstrapNodes, ","))
		s.PackSettings.SetBool("enable_dht", true)
	}

	if s.config.DisableUPNP == false {
		log.Info("Starting UPNP...")
		s.PackSettings.SetBool("enable_upnp", true)

		log.Info("Starting NATPMP...")
		s.PackSettings.SetBool("enable_natpmp", true)
	}

	s.Session.GetHandle().ApplySettings(s.PackSettings)

	for p := range s.mappedPorts {
		port, _ := strconv.Atoi(p)
		s.mappedPorts[p] = s.Session.GetHandle().AddPortMapping(lt.SessionHandleTcp, port, port)
		log.Infof("Adding port mapping %v: %v", port, s.mappedPorts[p])
	}
}

func (s *Service) stopServices() {
	if s.InternalProxy != nil {
		log.Infof("Stopping internal proxy")
		s.InternalProxy.Shutdown(nil)
		s.InternalProxy = nil
	}

	// TODO: cleanup these messages after windows hang is fixed
	// Don't need to execute RPC calls when Kodi is closing
	if s.dialogProgressBG != nil {
		log.Infof("Closing existing Dialog")
		s.dialogProgressBG.Close()
	}
	s.dialogProgressBG = nil

	// Try to clean dialogs in background to avoid getting deadlock because of already closed Kodi
	if !s.isShutdown {
		go func() {
			log.Infof("Cleaning up all DialogBG")
			xbmc.DialogProgressBGCleanup()

			log.Infof("Resetting RPC")
			xbmc.ResetRPC()
		}()
	}

	log.Info("Stopping LSD...")
	s.PackSettings.SetBool("enable_lsd", false)

	if s.config.DisableDHT == false {
		log.Info("Stopping DHT...")
		s.PackSettings.SetBool("enable_dht", false)
	}

	if s.config.DisableUPNP == false {
		log.Info("Stopping UPNP...")
		s.PackSettings.SetBool("enable_upnp", false)

		log.Info("Stopping NATPMP...")
		s.PackSettings.SetBool("enable_natpmp", false)
	}

	for p := range s.mappedPorts {
		port, _ := strconv.Atoi(p)
		s.Session.GetHandle().DeletePortMapping(s.mappedPorts[p])
		log.Infof("Deleting port mapping %v: %v", port, s.mappedPorts[p])
	}
	s.mappedPorts = map[string]int{}

	s.Session.GetHandle().ApplySettings(s.PackSettings)
}

// CheckAvailableSpace ...
func (s *Service) checkAvailableSpace(t *Torrent) bool {
	// For memory storage we don't need to check available space
	if s.IsMemoryStorage() {
		return true
	}

	diskStatus, err := diskusage.DiskUsage(config.Get().DownloadPath)
	if err != nil {
		log.Warningf("Unable to retrieve the free space for %s, continuing anyway...", config.Get().DownloadPath)
		return false
	}

	torrentInfo := t.th.TorrentFile()
	// defer lt.DeleteTorrentInfo(torrentInfo)

	if torrentInfo == nil || torrentInfo.Swigcptr() == 0 {
		log.Warning("Missing torrent info to check available space.")
		return false
	}

	status := t.th.Status(uint(lt.TorrentHandleQueryAccurateDownloadCounters) | uint(lt.TorrentHandleQuerySavePath))
	defer lt.DeleteTorrentStatus(status)

	totalSize := t.ti.TotalSize()
	totalDone := status.GetTotalDone()
	sizeLeft := totalSize - totalDone
	availableSpace := diskStatus.Free
	path := status.GetSavePath()

	log.Infof("Checking for sufficient space on %s...", path)
	log.Infof("Total size of download: %s", humanize.Bytes(uint64(totalSize)))
	log.Infof("All time download: %s", humanize.Bytes(uint64(status.GetAllTimeDownload())))
	log.Infof("Size total done: %s", humanize.Bytes(uint64(totalDone)))
	log.Infof("Size left to download: %s", humanize.Bytes(uint64(sizeLeft)))
	log.Infof("Available space: %s", humanize.Bytes(uint64(availableSpace)))

	if availableSpace < sizeLeft {
		log.Errorf("Unsufficient free space on %s. Has %d, needs %d.", path, diskStatus.Free, sizeLeft)
		xbmc.Notify("Elementum", "LOCALIZE[30207]", config.AddonIcon())

		log.Infof("Pausing torrent %s", t.th.Status(uint(lt.TorrentHandleQueryName)).GetName())
		t.Pause()
		return false
	}

	return true
}

// AddTorrent ...
func (s *Service) AddTorrent(uri string, paused bool) (*Torrent, error) {
	// To make sure no spaces coming from Web UI
	uri = strings.TrimSpace(uri)

	log.Infof("Adding torrent from %s", uri)

	if !s.IsMemoryStorage() && s.config.DownloadPath == "." {
		log.Warningf("Cannot add torrent since download path is not set")
		xbmc.Notify("Elementum", "LOCALIZE[30113]", config.AddonIcon())
		return nil, fmt.Errorf("Download path empty")
	}

	torrentParams := lt.NewAddTorrentParams()
	defer lt.DeleteAddTorrentParams(torrentParams)

	if s.IsMemoryStorage() {
		torrentParams.SetMemoryStorage(s.GetMemorySize())
	}

	torrentParams.SetMaxConnections(getPlatformSpecificConnectionLimit())

	var err error
	var th lt.TorrentHandle
	var infoHash string

	// Dummy check if torrent file is a file containing a magnet link
	if _, err := os.Stat(uri); err == nil {
		dat, err := ioutil.ReadFile(uri)
		if err == nil && bytes.HasPrefix(dat, []byte("magnet:")) {
			uri = string(dat)
		}
	}

	if strings.HasPrefix(uri, "magnet:") {
		// Remove all spaces in magnet
		uri = strings.Replace(uri, " ", "", -1)

		torrent := NewTorrentFile(uri)

		if torrent.IsMagnet() {
			torrent.Magnet()

			log.Infof("Using modified magnet: %s", torrent.URI)
			if err := torrent.IsValidMagnet(); err == nil {
				torrentParams.SetUrl(torrent.URI)
			} else {
				return nil, err
			}
		} else {
			torrent.Resolve()
		}

		uri = torrent.URI
		infoHash = torrent.InfoHash
	} else {
		if strings.HasPrefix(uri, "http") {
			torrent := NewTorrentFile(uri)

			if err = torrent.Resolve(); err != nil {
				log.Warningf("Could not resolve torrent %s: %#v", uri, err)
				return nil, err
			}
			uri = torrent.URI
		}

		log.Debugf("Adding torrent: %#v", uri)

		info := lt.NewTorrentInfo(uri)
		defer lt.DeleteTorrentInfo(info)
		torrentParams.SetTorrentInfo(info)

		shaHash := info.InfoHash().ToString()
		infoHash = hex.EncodeToString([]byte(shaHash))
	}

	log.Infof("Setting save path to %s", s.config.DownloadPath)
	torrentParams.SetSavePath(s.config.DownloadPath)

	if !s.IsMemoryStorage() {
		log.Infof("Checking for fast resume data in %s.fastresume", infoHash)
		fastResumeFile := filepath.Join(s.config.TorrentsPath, fmt.Sprintf("%s.fastresume", infoHash))
		if _, err := os.Stat(fastResumeFile); err == nil {
			log.Info("Found fast resume data")
			fastResumeData, err := ioutil.ReadFile(fastResumeFile)
			if err != nil {
				return nil, err
			}

			fastResumeVector := lt.NewStdVectorChar()
			defer lt.DeleteStdVectorChar(fastResumeVector)
			for _, c := range fastResumeData {
				fastResumeVector.Add(c)
			}
			torrentParams.SetResumeData(fastResumeVector)
		}
	}

	// Setting default priorities to 0 to avoid downloading non-wanted files
	filesPriorities := lt.NewStdVectorInt()
	defer lt.DeleteStdVectorInt(filesPriorities)
	for i := 0; i <= 500; i++ {
		filesPriorities.Add(0)
	}
	torrentParams.SetFilePriorities(filesPriorities)

	// Call torrent creation
	th = s.Session.GetHandle().AddTorrent(torrentParams)
	if !paused {
		th.Resume()
	}

	log.Infof("Setting sequential download to: %v", !s.IsMemoryStorage())
	th.SetSequentialDownload(!s.IsMemoryStorage())

	log.Infof("Adding new torrent item with url: %s", uri)
	t := NewTorrent(s, th, th.TorrentFile(), uri)

	if s.IsMemoryStorage() {
		t.MemorySize = s.GetMemorySize()
	}

	t.addedTime = time.Now()
	s.q.Add(t)

	if !t.HasMetadata() {
		log.Infof("Waiting for information fetched for torrent: %s", infoHash)
		<-t.GotInfo()
		log.Infof("Information fetched for torrent: %s", infoHash)
	}

	// Saving torrent file
	t.onMetadataReceived()

	go t.Watch()

	return t, nil
}

// RemoveTorrent ...
func (s *Service) RemoveTorrent(torrent *Torrent, removeFiles bool) bool {
	log.Debugf("Removing torrent: %s", torrent.Name())
	if torrent == nil {
		return false
	}

	defer func() {
		database.Get().DeleteBTItem(torrent.InfoHash())
	}()

	if t := s.q.FindByHash(torrent.InfoHash()); t != nil {
		s.q.Delete(torrent)

		t.Drop(removeFiles)
		return true
	}

	return false
}

func (s *Service) onStateChanged(stateAlert lt.StateChangedAlert) {
	switch stateAlert.GetState() {
	case lt.TorrentStatusDownloading:
		torrentHandle := stateAlert.GetHandle()
		torrentStatus := torrentHandle.Status(uint(lt.TorrentHandleQueryName))
		shaHash := torrentStatus.GetInfoHash().ToString()
		infoHash := hex.EncodeToString([]byte(shaHash))
		if spaceChecked, exists := s.SpaceChecked[infoHash]; exists {
			if spaceChecked == false {
				if t := s.GetTorrentByHash(infoHash); t != nil {
					s.checkAvailableSpace(t)
					delete(s.SpaceChecked, infoHash)
				}
			}
		}
	}
}

// GetTorrentByHash ...
func (s *Service) GetTorrentByHash(hash string) *Torrent {
	return s.q.FindByHash(hash)
}

func (s *Service) saveResumeDataLoop() {
	saveResumeWait := time.NewTicker(time.Duration(s.config.SessionSave) * time.Second)
	closing := s.Closer.C()
	defer saveResumeWait.Stop()

	for {
		select {
		case <-closing:
			return
		case <-saveResumeWait.C:
			torrentsVector := s.Session.GetHandle().GetTorrents()
			torrentsVectorSize := int(torrentsVector.Size())

			for i := 0; i < torrentsVectorSize; i++ {
				torrentHandle := torrentsVector.Get(i)
				if torrentHandle.IsValid() == false {
					continue
				}

				status := torrentHandle.Status()
				defer lt.DeleteTorrentStatus(status)

				if status.GetHasMetadata() == false || status.GetNeedSaveResume() == false {
					continue
				}

				torrentHandle.SaveResumeData(1)
			}
		}
	}
}

func (s *Service) saveResumeDataConsumer() {
	alerts, alertsDone := s.Alerts()
	closing := s.Closer.C()
	defer close(alertsDone)

	for {
		select {
		case <-closing:
			return
		case alert, ok := <-alerts:
			if !ok { // was the alerts channel closed?
				return
			}
			switch alert.Type {
			case lt.StateChangedAlertAlertType:
				stateAlert := lt.SwigcptrStateChangedAlert(alert.Pointer)
				s.onStateChanged(stateAlert)

			case lt.SaveResumeDataAlertAlertType:
				bEncoded := []byte(lt.Bencode(alert.Entry))
				b := bytes.NewReader(bEncoded)
				dec := bencode.NewDecoder(b)
				var torrentFile *TorrentFileRaw
				if err := dec.Decode(&torrentFile); err != nil {
					log.Warningf("Resume data corrupted for %s, %d bytes received and failed to decode with: %s, skipping...", alert.Name, len(bEncoded), err.Error())
				} else {
					path := filepath.Join(s.config.TorrentsPath, fmt.Sprintf("%s.fastresume", alert.InfoHash))
					ioutil.WriteFile(path, bEncoded, 0644)
				}
			}
		}
	}
}

func (s *Service) alertsConsumer() {
	closing := s.Closer.C()
	defer s.alertsBroadcaster.Close()

	ltOneSecond := lt.Seconds(ltAlertWaitTime)
	log.Info("Consuming alerts...")
	for {
		select {
		case <-closing:
			log.Info("Closing all alert channels...")

			return
		default:
			if s.Session.GetHandle().WaitForAlert(ltOneSecond).Swigcptr() == 0 {
				continue
			} else if s.Closer.IsSet() {
				return
			}

			var alerts lt.StdVectorAlerts
			alerts = s.Session.GetHandle().PopAlerts()
			queueSize := alerts.Size()
			var name string
			var infoHash string
			var entry lt.Entry
			for i := 0; i < int(queueSize); i++ {
				ltAlert := alerts.Get(i)
				alertType := ltAlert.Type()
				alertPtr := ltAlert.Swigcptr()
				alertMessage := ltAlert.Message()

				switch alertType {
				case lt.SaveResumeDataAlertAlertType:
					saveResumeData := lt.SwigcptrSaveResumeDataAlert(alertPtr)
					torrentHandle := saveResumeData.GetHandle()
					torrentStatus := torrentHandle.Status(uint(lt.TorrentHandleQuerySavePath) | uint(lt.TorrentHandleQueryName))
					name = torrentStatus.GetName()
					shaHash := torrentStatus.GetInfoHash().ToString()
					infoHash = hex.EncodeToString([]byte(shaHash))
					entry = saveResumeData.ResumeData()
				case lt.ExternalIpAlertAlertType:
					splitMessage := strings.Split(alertMessage, ":")
					splitIP := strings.Split(splitMessage[len(splitMessage)-1], ".")
					alertMessage = strings.Join(splitMessage[:len(splitMessage)-1], ":") + splitIP[0] + ".XX.XX.XX"
				case lt.MetadataReceivedAlertAlertType:
					metadataAlert := lt.SwigcptrMetadataReceivedAlert(alertPtr)
					for _, t := range s.q.All() {
						if t.th != nil && metadataAlert.GetHandle().Equal(t.th) {
							t.gotMetainfo.Set()
						}
					}
				}

				alert := &Alert{
					Type:     alertType,
					Category: ltAlert.Category(),
					What:     ltAlert.What(),
					Message:  alertMessage,
					Pointer:  alertPtr,
					Name:     name,
					Entry:    entry,
					InfoHash: infoHash,
				}
				s.alertsBroadcaster.Broadcast(alert)
			}
		}
	}
}

// Alerts ...
func (s *Service) Alerts() (<-chan *Alert, chan<- interface{}) {
	c, done := s.alertsBroadcaster.Listen()
	ac := make(chan *Alert)
	go func() {
		for v := range c {
			ac <- v.(*Alert)
		}
	}()
	return ac, done
}

func (s *Service) logAlerts() {
	alerts, _ := s.Alerts()
	for alert := range alerts {
		// Skipping Tracker communication, Save_Resume, UDP errors
		// No need to spam logs.
		if alert.Category&int(lt.SaveResumeDataAlertAlertType) != 0 || alert.Category&int(lt.UdpErrorAlertAlertType) != 0 || alert.Category&int(lt.AlertBlockProgressNotification) != 0 {
			continue
		} else if alert.Category&int(lt.AlertErrorNotification) != 0 {
			log.Errorf("%s: %s", alert.What, alert.Message)
		} else if alert.Category&int(lt.AlertDebugNotification) != 0 {
			log.Debugf("%s: %s", alert.What, alert.Message)
		} else if alert.Category&int(lt.AlertPerformanceWarning) != 0 {
			log.Warningf("%s: %s", alert.What, alert.Message)
		} else {
			log.Noticef("%s: %s", alert.What, alert.Message)
		}
	}
}

func (s *Service) loadTorrentFiles() {
	// Cleaning the queue
	s.q.Clean()

	// Not loading previous torrents on start
	// Otherwise we can dig out all the memory and halt the device
	if s.IsMemoryStorage() || !s.config.AutoloadTorrents {
		return
	}

	log.Infof("Loading torrents from: %s", s.config.TorrentsPath)
	files, err := ioutil.ReadDir(s.config.TorrentsPath)
	if err != nil {
		log.Infof("Cannot read torrents dir: %s", err)
		return
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].ModTime().Unix() < files[j].ModTime().Unix()
	})

	for _, torrentFile := range files {
		if !strings.HasSuffix(torrentFile.Name(), ".torrent") {
			continue
		}

		filePath := filepath.Join(s.config.TorrentsPath, torrentFile.Name())
		log.Infof("Loading torrent file %s", torrentFile.Name())

		torrentParams := lt.NewAddTorrentParams()
		defer lt.DeleteAddTorrentParams(torrentParams)

		t, _ := s.AddTorrent(filePath, s.config.AutoloadTorrentsPaused)
		if t != nil {
			i := database.Get().GetBTItem(t.InfoHash())
			if i != nil {
				t.DBItem = i

				for _, p := range i.Files {
					if f := t.GetFileByPath(p); f != nil {
						t.DownloadFile(f)
					}
				}
			}
		}
	}

	s.cleanStaleFiles(s.config.DownloadPath, ".parts")
	s.cleanStaleFiles(s.config.TorrentsPath, ".fastresume")
}

func (s *Service) cleanStaleFiles(dir string, ext string) {
	log.Infof("Cleaning up stale %s files at %s ...", ext, dir)

	staleFiles, _ := filepath.Glob(filepath.Join(dir, "*"+ext))
	for _, staleFile := range staleFiles {
		infoHash := strings.Replace(strings.Replace(staleFile, dir, "", 1), ext, "", 1)[1:]
		if infoHash[0] == '.' {
			infoHash = strings.Replace(strings.Replace(staleFile, dir, "", 1), ext, "", 1)[2:]
		}

		if t := s.GetTorrentByHash(infoHash); t != nil {
			continue
		}

		if err := os.Remove(staleFile); err != nil {
			log.Error(err)
		} else {
			log.Info("Removed", staleFile)
		}
	}
}

func (s *Service) downloadProgress() {
	closing := s.Closer.C()
	rotateTicker := time.NewTicker(5 * time.Second)
	defer rotateTicker.Stop()

	pathChecked := make(map[string]bool)
	warnedMissing := make(map[string]bool)

	showNext := 0
	for {
		select {
		case <-closing:
			return

		case <-rotateTicker.C:
			// TODO: there should be a check whether service is in Pause state
			// if !s.config.DisableBgProgress && s.dialogProgressBG != nil {
			// 	s.dialogProgressBG.Close()
			// 	s.dialogProgressBG = nil
			// 	continue
			// }

			if s.Closer.IsSet() || s.Session == nil || s.Session.GetHandle() == nil {
				return
			}

			var totalDownloadRate float64
			var totalUploadRate float64
			var totalProgress int

			activeTorrents := make([]*activeTorrent, 0)
			torrentsVector := s.Session.GetHandle().GetTorrents()
			torrentsVectorSize := int(torrentsVector.Size())

			for i := 0; i < torrentsVectorSize; i++ {
				torrentHandle := torrentsVector.Get(i)
				if torrentHandle.IsValid() == false {
					continue
				}

				ts := torrentHandle.Status()
				defer lt.DeleteTorrentStatus(ts)

				if ts.GetHasMetadata() == false || s.Session.GetHandle().IsPaused() {
					continue
				}

				shaHash := ts.GetInfoHash().ToString()
				infoHash := hex.EncodeToString([]byte(shaHash))

				status := StatusStrings[int(ts.GetState())]
				isPaused := ts.GetPaused()

				if t := s.GetTorrentByHash(infoHash); t != nil {
					status = t.GetStateString()
				}

				downloadRate := float64(ts.GetDownloadPayloadRate())
				uploadRate := float64(ts.GetUploadPayloadRate())
				totalDownloadRate += downloadRate
				totalUploadRate += uploadRate

				torrentName := ts.GetName()
				progress := int(float64(ts.GetProgress()) * 100)

				if progress < 100 && !isPaused {
					activeTorrents = append(activeTorrents, &activeTorrent{
						torrentName:  torrentName,
						downloadRate: downloadRate,
						uploadRate:   uploadRate,
						progress:     progress,
					})
					totalProgress += progress
					continue
				}

				seedingTime := ts.GetSeedingTime()
				finishedTime := ts.GetFinishedTime()
				if progress == 100 && seedingTime == 0 {
					seedingTime = finishedTime
				}

				if !s.IsMemoryStorage() && s.config.SeedTimeLimit > 0 {
					if seedingTime >= s.config.SeedTimeLimit {
						if !isPaused {
							log.Warningf("Seeding time limit reached, pausing %s", torrentName)
							torrentHandle.AutoManaged(false)
							torrentHandle.Pause(1)
							isPaused = true
						}
						status = "Seeded"
					}
				}
				if !s.IsMemoryStorage() && s.config.SeedTimeRatioLimit > 0 {
					timeRatio := 0
					downloadTime := ts.GetActiveTime() - seedingTime
					if downloadTime > 1 {
						timeRatio = seedingTime * 100 / downloadTime
					}
					if timeRatio >= s.config.SeedTimeRatioLimit {
						if !isPaused {
							log.Warningf("Seeding time ratio reached, pausing %s", torrentName)
							torrentHandle.AutoManaged(false)
							torrentHandle.Pause(1)
							isPaused = true
						}
						status = "Seeded"
					}
				}
				if !s.IsMemoryStorage() && s.config.ShareRatioLimit > 0 {
					ratio := int64(0)
					allTimeDownload := ts.GetAllTimeDownload()
					if allTimeDownload > 0 {
						ratio = ts.GetAllTimeUpload() * 100 / allTimeDownload
					}
					if ratio >= int64(s.config.ShareRatioLimit) {
						if !isPaused {
							log.Warningf("Share ratio reached, pausing %s", torrentName)
							torrentHandle.AutoManaged(false)
							torrentHandle.Pause(1)
						}
						status = "Seeded"
					}
				}

				if s.MarkedToMove != "" && infoHash == s.MarkedToMove {
					s.MarkedToMove = ""
					status = "Seeded"
				}

				//
				// Handle moving completed downloads
				//
				if !s.config.CompletedMove || status != "Seeded" || s.anyPlayerIsPlaying() {
					continue
				}
				if xbmc.PlayerIsPlaying() {
					continue
				}

				if _, exists := warnedMissing[infoHash]; exists {
					continue
				}

				func() error {
					item := database.Get().GetBTItem(infoHash)
					if item == nil {
						warnedMissing[infoHash] = true
						return fmt.Errorf("Torrent not found with infohash: %s", infoHash)
					}

					errMsg := fmt.Sprintf("Missing item type to move files to completed folder for %s", torrentName)
					if item.Type == "" {
						log.Error(errMsg)
						return errors.New(errMsg)
					}
					log.Warning(torrentName, "finished seeding, moving files...")

					// Check paths are valid and writable, and only once
					if _, exists := pathChecked[item.Type]; !exists {
						if item.Type == "movie" {
							if err := config.IsWritablePath(s.config.CompletedMoviesPath); err != nil {
								warnedMissing[infoHash] = true
								pathChecked[item.Type] = true
								log.Error(err)
								return err
							}
							pathChecked[item.Type] = true
						} else {
							if err := config.IsWritablePath(s.config.CompletedShowsPath); err != nil {
								warnedMissing[infoHash] = true
								pathChecked[item.Type] = true
								log.Error(err)
								return err
							}
							pathChecked[item.Type] = true
						}
					}

					log.Info("Removing the torrent without deleting files after Completed move ...")
					t := s.GetTorrentByHash(infoHash)
					s.RemoveTorrent(t, false)

					// Delete leftover .parts file if any
					partsFile := filepath.Join(config.Get().DownloadPath, fmt.Sprintf(".%s.parts", infoHash))
					os.Remove(partsFile)

					// Delete fast resume data
					fastResumeFile := filepath.Join(s.config.TorrentsPath, fmt.Sprintf("%s.fastresume", infoHash))
					if _, err := os.Stat(fastResumeFile); err == nil {
						log.Info("Deleting fast resume data at", fastResumeFile)
						if err := os.Remove(fastResumeFile); err != nil {
							log.Error(err)
							return err
						}
					}

					// Delete torrent file
					torrentFile := filepath.Join(s.config.TorrentsPath, fmt.Sprintf("%s.torrent", infoHash))
					if _, err := os.Stat(torrentFile); err == nil {
						log.Info("Deleting torrent file at ", torrentFile)
						if err := os.Remove(torrentFile); err != nil {
							log.Error(err)
							return err
						}
					}

					if len(item.Files) <= 0 {
						return errors.New("No files saved for BTItem")
					}

					torrentInfo := torrentHandle.TorrentFile()
					for _, fp := range item.Files {
						f := t.GetFileByPath(fp)

						filePath := torrentInfo.Files().FilePath(f.Index)
						fileName := filepath.Base(filePath)

						extracted := ""
						re := regexp.MustCompile("(?i).*\\.rar")
						if re.MatchString(fileName) {
							extractedPath := filepath.Join(s.config.DownloadPath, filepath.Dir(filePath), "extracted")
							files, err := ioutil.ReadDir(extractedPath)
							if err != nil {
								return err
							}
							if len(files) == 1 {
								extracted = files[0].Name()
							} else {
								for _, file := range files {
									fileNameCurrent := file.Name()
									re := regexp.MustCompile("(?i).*\\.(mkv|mp4|mov|avi)")
									if re.MatchString(fileNameCurrent) {
										extracted = fileNameCurrent
										break
									}
								}
							}
							if extracted != "" {
								filePath = filepath.Join(filepath.Dir(filePath), "extracted", extracted)
							} else {
								return errors.New("No extracted file to move")
							}
						}

						var dstPath string
						if item.Type == "movie" {
							dstPath = filepath.Dir(s.config.CompletedMoviesPath)
						} else {
							dstPath = filepath.Dir(s.config.CompletedShowsPath)
							if item.ShowID > 0 {
								show := tmdb.GetShow(item.ShowID, config.Get().Language)
								if show != nil {
									showPath := util.ToFileName(fmt.Sprintf("%s (%s)", show.Name, strings.Split(show.FirstAirDate, "-")[0]))
									seasonPath := filepath.Join(showPath, fmt.Sprintf("Season %d", item.Season))
									if item.Season == 0 {
										seasonPath = filepath.Join(showPath, "Specials")
									}
									dstPath = filepath.Join(dstPath, seasonPath)
									os.MkdirAll(dstPath, 0755)
								}
							}
						}

						go func() {
							log.Infof("Moving %s to %s", fileName, dstPath)
							srcPath := filepath.Join(s.config.DownloadPath, filePath)
							if dst, err := util.Move(srcPath, dstPath); err != nil {
								log.Error(err)
							} else {
								// Remove leftover folders
								if dirPath := filepath.Dir(filePath); dirPath != "." {
									os.RemoveAll(filepath.Dir(srcPath))
									if extracted != "" {
										parentPath := filepath.Clean(filepath.Join(filepath.Dir(srcPath), ".."))
										if parentPath != "." && parentPath != s.config.DownloadPath {
											os.RemoveAll(parentPath)
										}
									}
								}
								log.Warning(fileName, "moved to", dst)

								log.Infof("Marking %s for removal from library and database...", torrentName)
								database.Get().UpdateBTItemStatus(infoHash, Remove)
							}
						}()
					}
					return nil
				}()
			}

			totalActive := len(activeTorrents)
			if totalActive > 0 {
				showProgress := totalProgress / totalActive
				showTorrent := fmt.Sprintf("Total - D/L: %s - U/L: %s", humanize.Bytes(uint64(totalDownloadRate))+"/s", humanize.Bytes(uint64(totalUploadRate))+"/s")
				if showNext >= totalActive {
					showNext = 0
				} else {
					showProgress = activeTorrents[showNext].progress
					torrentName := activeTorrents[showNext].torrentName
					if len(torrentName) > 30 {
						torrentName = torrentName[:30] + "..."
					}
					showTorrent = fmt.Sprintf("%s - %s - %s", torrentName, humanize.Bytes(uint64(activeTorrents[showNext].downloadRate))+"/s", humanize.Bytes(uint64(activeTorrents[showNext].uploadRate))+"/s")
					showNext++
				}
				if !s.config.DisableBgProgress && (!s.config.DisableBgProgressPlayback || !s.anyPlayerIsPlaying()) {
					if s.dialogProgressBG == nil {
						s.dialogProgressBG = xbmc.NewDialogProgressBG("Elementum", "")
					}
					if s.dialogProgressBG != nil {
						s.dialogProgressBG.Update(showProgress, "Elementum", showTorrent)
					}
				}
			} else if (!s.config.DisableBgProgress || (s.config.DisableBgProgressPlayback && s.anyPlayerIsPlaying())) && s.dialogProgressBG != nil {
				s.dialogProgressBG.Close()
				s.dialogProgressBG = nil
			}
		}
	}
}

// SetDownloadLimit ...
func (s *Service) SetDownloadLimit(i int) {
	settings := s.PackSettings
	settings.SetInt("download_rate_limit", i)

	s.Session.GetHandle().ApplySettings(settings)
}

// SetUploadLimit ...
func (s *Service) SetUploadLimit(i int) {
	settings := s.PackSettings

	settings.SetInt("upload_rate_limit", i)
	s.Session.GetHandle().ApplySettings(settings)
}

// RestoreLimits ...
func (s *Service) RestoreLimits() {
	if s.config.DownloadRateLimit > 0 {
		s.SetDownloadLimit(s.config.DownloadRateLimit)
		log.Infof("Rate limiting download to %s", humanize.Bytes(uint64(s.config.DownloadRateLimit)))
	} else {
		s.SetDownloadLimit(0)
	}

	// if s.config.DisableUpload {
	// 	s.SetUploadLimit(1)
	// 	log.Infof("Rate limiting upload to %d byte, due to disabled upload", 1)
	// } else if s.config.UploadRateLimit > 0 {
	if s.config.UploadRateLimit > 0 {
		s.SetUploadLimit(s.config.UploadRateLimit)
		log.Infof("Rate limiting upload to %s", humanize.Bytes(uint64(s.config.UploadRateLimit)))
	} else {
		s.SetUploadLimit(0)
	}
}

// SetBufferingLimits ...
func (s *Service) SetBufferingLimits() {
	if s.config.LimitAfterBuffering {
		s.SetDownloadLimit(0)
		log.Info("Resetting rate limited download for buffering")
	}
}

// GetSeedTime ...
func (s *Service) GetSeedTime() int64 {
	if s.config.DisableUpload {
		return 0
	}

	return int64(s.config.SeedTimeLimit)
}

// GetBufferSize ...
func (s *Service) GetBufferSize() int64 {
	b := int64(s.config.BufferSize)
	if b < EndBufferSize {
		return EndBufferSize
	}
	return b
}

// GetMemorySize ...
func (s *Service) GetMemorySize() int64 {
	return int64(config.Get().MemorySize)
}

// GetStorageType ...
func (s *Service) GetStorageType() int {
	return s.config.DownloadStorage
}

// PlayerStop ...
func (s *Service) PlayerStop() {
	log.Debugf("PlayerStop")
}

// PlayerSeek ...
func (s *Service) PlayerSeek() {
	log.Debugf("PlayerSeek")
}

// ClientInfo ...
func (s *Service) ClientInfo(_w io.Writer) {
	// TODO: Print any client info here
	// w := bufio.NewWriter(_w)

	// for _, t := range s.q.All() {
	// 	if t == nil || t.th == nil {
	// 		continue
	// 	}

	// 	st := t.th.Status()
	// 	defer lt.DeleteTorrentStatus(st)

	// 	if st == nil || st.Swigcptr() == 0 {
	// 		continue
	// 	}

	// }

}

// AttachPlayer adds Player instance to service
func (s *Service) AttachPlayer(p *Player) {
	if p == nil || p.t == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.Players[p.t.InfoHash()]; ok {
		return
	}

	s.Players[p.t.InfoHash()] = p
}

// DetachPlayer removes Player instance
func (s *Service) DetachPlayer(p *Player) {
	if p == nil || p.t == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.Players, p.t.InfoHash())
}

// GetPlayer searches for player with desired TMDB id
func (s *Service) GetPlayer(kodiID int, tmdbID int) *Player {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, p := range s.Players {
		if p == nil || p.t == nil {
			continue
		}

		if (tmdbID != 0 && p.p.TMDBId == tmdbID) || (kodiID != 0 && p.p.KodiID == kodiID) {
			return p
		}
	}

	return nil
}

func (s *Service) anyPlayerIsPlaying() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, p := range s.Players {
		if p == nil || p.t == nil {
			continue
		}

		if p.p.Playing {
			return true
		}
	}

	return false
}

// GetActivePlayer searches for player that is Playing anything
func (s *Service) GetActivePlayer() *Player {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, p := range s.Players {
		if p == nil || p.t == nil {
			continue
		}

		if p.p.Playing {
			return p
		}
	}

	return nil
}

// HasTorrentByID checks whether there is active torrent for queried tmdb id
func (s *Service) HasTorrentByID(tmdbID int) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, t := range s.q.All() {
		if t == nil || t.DBItem == nil {
			continue
		}

		if t.DBItem.ID == tmdbID {
			return t.InfoHash()
		}
	}

	return ""
}

// HasTorrentByQuery checks whether there is active torrent with searches query
func (s *Service) HasTorrentByQuery(query string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, t := range s.q.All() {
		if t == nil || t.DBItem == nil {
			continue
		}

		if t.DBItem.Query == query {
			return t.InfoHash()
		}
	}

	return ""
}

// HasTorrentBySeason checks whether there is active torrent for queried season
func (s *Service) HasTorrentBySeason(tmdbID int, season int) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, t := range s.q.All() {
		if t == nil || t.DBItem == nil {
			continue
		}

		if t.DBItem.ShowID == tmdbID && t.DBItem.Season == season {
			return t.InfoHash()
		}
	}

	return ""
}

// HasTorrentByEpisode checks whether there is active torrent for queried episode
func (s *Service) HasTorrentByEpisode(tmdbID int, season, episode int) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	re := regexp.MustCompile(fmt.Sprintf(episodeMatchRegex, season, episode))

	for _, t := range s.q.All() {
		if t == nil || t.DBItem == nil {
			continue
		}

		if t.DBItem.ShowID == tmdbID && t.DBItem.Season == season && t.DBItem.Episode == episode {
			// This is a strict match
			return t.InfoHash(), t.IsNextEpisode
		} else if t.DBItem.ShowID == tmdbID {
			// Try to find an episode
			for _, choice := range t.files {
				if re.MatchString(choice.Path) {
					return t.InfoHash(), t.IsNextEpisode
				}
			}
		}
	}

	return "", false
}

// HasTorrentByName checks whether there is active torrent for queried name
func (s *Service) HasTorrentByName(query string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, t := range s.q.All() {
		if t == nil {
			continue
		}

		if strings.Contains(t.Name(), query) {
			return t.InfoHash()
		}
	}

	return ""
}

// GetTorrentByFakeID checks whether there is active torrent with fake id
func (s *Service) GetTorrentByFakeID(query string) *Torrent {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, t := range s.q.All() {
		if t == nil || t.DBItem == nil {
			continue
		}

		id := strconv.FormatUint(xxhash.Sum64String(t.DBItem.Query), 10)
		if id == query {
			return t
		}
	}

	return nil
}

// GetTorrents return all active torrents
func (s *Service) GetTorrents() []*Torrent {
	return s.q.All()
}

// GetListenIP returns calculated IP for TCP/TCP6
func (s *Service) GetListenIP(network string) string {
	if strings.Contains(network, "6") {
		return s.ListenIPv6
	}
	return s.ListenIP
}

// GetMemoryStats returns total and free memory sizes for this OS
func (s *Service) GetMemoryStats() (int64, int64) {
	v, _ := mem.VirtualMemory()
	return int64(v.Total), int64(v.Free)
}

// IsMemoryStorage is a shortcut for checking whether we run memory storage
func (s *Service) IsMemoryStorage() bool {
	return s.config.DownloadStorage == StorageMemory
}

// watchConfig watches for libtorrent.config changes to reapply libtorrent settings
func (s *Service) watchConfig() {
	w := watcher.New()

	go func() {
		closing := s.Closer.C()

		for {
			select {
			case event := <-w.Event:
				log.Infof("Watcher notify: %v", event)
				s.configure()
				s.applyCustomSettings()
			case err := <-w.Error:
				log.Errorf("Watcher error: %s", err)
			case <-w.Closed:
				return
			case <-closing:
				w.Close()
				return
			}
		}
	}()

	filePath := filepath.Join(config.Get().ProfilePath, "libtorrent.config")
	if err := w.Add(filePath); err != nil {
		log.Errorf("Watcher error. Could not add file to watch: %s", err)
	}

	if err := w.Start(time.Millisecond * 500); err != nil {
		log.Errorf("Error watching files: %s", err)
	}
}

func (s *Service) applyCustomSettings() {
	if !s.config.UseLibtorrentConfig {
		return
	}

	settings := s.PackSettings

	for k, v := range s.readCustomSettings() {
		if v == "true" {
			settings.SetBool(k, true)
			log.Infof("Applying bool setting: %s=true", k)
			continue
		} else if v == "false" {
			settings.SetBool(k, false)
			log.Infof("Applying bool setting: %s=false", k)
			continue
		} else if in, err := strconv.Atoi(v); err == nil {
			settings.SetInt(k, in)
			log.Infof("Applying int setting: %s=%d", k, in)
			continue
		}

		log.Errorf("Cannot parse config settings for: %s=%s", k, v)
	}

	s.Session.GetHandle().ApplySettings(settings)
}

func (s *Service) readCustomSettings() map[string]string {
	ret := map[string]string{}

	filePath := filepath.Join(config.Get().ProfilePath, "libtorrent.config")
	f, err := os.Open(filePath)
	if err != nil {
		return ret
	}
	defer f.Close()

	reReplace := regexp.MustCompile(`[^_\d\w=]`)
	reFind := regexp.MustCompile(`([_\d\w=]+)=(\w+)`)
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		l := scan.Text()

		l = strings.Replace(l, " ", "", -1)
		if strings.HasPrefix(l, "#") {
			continue
		}

		l = reReplace.ReplaceAllString(l, "")
		res := reFind.FindStringSubmatch(l)
		if len(res) < 3 {
			continue
		}

		ret[res[1]] = res[2]
	}

	return ret
}
