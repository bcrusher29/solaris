package bittorrent

import (
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/dustin/go-humanize"
	"github.com/op/go-logging"
	"github.com/zeebo/bencode"

	"github.com/bcrusher29/solaris/config"
	"github.com/bcrusher29/solaris/scrape"
	"github.com/bcrusher29/solaris/util"
	"github.com/bcrusher29/solaris/xbmc"
)

var torrentFileLog = logging.MustGetLogger("torrentFile")

// TorrentFile represents a physical torrent file
type TorrentFile struct {
	URI        string   `json:"uri"`
	InfoHash   string   `json:"info_hash"`
	Title      string   `json:"title"`
	Name       string   `json:"name"`
	Trackers   []string `json:"trackers"`
	Size       string   `json:"size"`
	SizeParsed uint64   `jsin:"-"`
	Seeds      int64    `json:"seeds"`
	Peers      int64    `json:"peers"`
	IsPrivate  bool     `json:"is_private"`
	Provider   string   `json:"provider"`
	Icon       string   `json:"icon"`
	Multi      bool

	Resolution  int    `json:"resolution"`
	VideoCodec  int    `json:"video_codec"`
	AudioCodec  int    `json:"audio_codec"`
	Language    string `json:"language"`
	RipType     int    `json:"rip_type"`
	SceneRating int    `json:"scene_rating"`

	hasResolved bool
}

// Used to avoid infinite recursion in UnmarshalJSON
type torrent TorrentFile

// TorrentFileRaw ...
type TorrentFileRaw struct {
	Title        string                 `bencode:"title"`
	Announce     string                 `bencode:"announce"`
	AnnounceList [][]string             `bencode:"announce-list"`
	Info         map[string]interface{} `bencode:"info"`
}

const (
	// ResolutionUnknown ...
	ResolutionUnknown = iota
	// Resolution240p ...
	Resolution240p
	// Resolution480p ...
	Resolution480p
	// Resolution720p ...
	Resolution720p
	// Resolution1080p ...
	Resolution1080p
	// Resolution2K ...
	Resolution2K
	// Resolution4k ...
	Resolution4k
)

var (
	// [pр] actually contains "p" from latin and "р" from cyrillic, which looks the same, but it's not.
	resolutionTags = []map[*regexp.Regexp]int{
		map[*regexp.Regexp]int{regexp.MustCompile(`(?i)\W+240[pр]\W*`): Resolution240p},
		map[*regexp.Regexp]int{regexp.MustCompile(`(?i)\W+480[pр]\W*`): Resolution480p},
		map[*regexp.Regexp]int{regexp.MustCompile(`(?i)\W+(720[pр]|1280x720)\W*`): Resolution720p},
		map[*regexp.Regexp]int{regexp.MustCompile(`(?i)\W+(1080[piр]|1920x1080)\W*`): Resolution1080p},
		map[*regexp.Regexp]int{regexp.MustCompile(`(?i)\W+1440[pр]\W*`): Resolution2K},
		map[*regexp.Regexp]int{regexp.MustCompile(`(?i)\W+(4k|2160[pр]|UHD)\W*`): Resolution4k},

		map[*regexp.Regexp]int{regexp.MustCompile(`(?i)\W+(vhs\-?rip)\W*`): Resolution240p},
		map[*regexp.Regexp]int{regexp.MustCompile(`(?i)\W+(tv\-?rip|sat\-?rip|iptv\-?rip|xvid|dvd|hdtv|web\-(dl)?rip)\W*`): Resolution480p},
		map[*regexp.Regexp]int{regexp.MustCompile(`(?i)\W+(hd720p?|hd\-?rip|b[rd]rip)\W*`): Resolution720p},
		map[*regexp.Regexp]int{regexp.MustCompile(`(?i)\W+(hd1080p?|fullhd|fhd|blu\W*ray|bd\W*remux)\W*`): Resolution1080p},
		map[*regexp.Regexp]int{regexp.MustCompile(`(?i)\W+(2k)\W*`): Resolution2K},
		map[*regexp.Regexp]int{regexp.MustCompile(`(?i)\W+(4k|hd4k)\W*`): Resolution4k},
	}
	// Resolutions ...
	Resolutions = []string{"", "240p", "480p", "720p", "1080p", "2K", "4K"}
	// Colors ...
	Colors = []string{"", "FFFC3401", "FFA56F01", "FF539A02", "FF0166FC", "FFF15052", "FF6BB9EC"}
	// Size regexp
	sizeMatcher = regexp.MustCompile(`^\s*([\d\.\,]+)\s*`)
)

const (
	// RipUnknown ...
	RipUnknown = iota
	// RipCam ...
	RipCam
	// RipTS ...
	RipTS
	// RipTC ...
	RipTC
	// RipScr ...
	RipScr
	// RipDVDScr ...
	RipDVDScr
	// RipDVD ...
	RipDVD
	// RipHDTV ...
	RipHDTV
	// RipWeb ...
	RipWeb
	// RipBluRay ...
	RipBluRay
)

var (
	ripTags = map[*regexp.Regexp]int{
		regexp.MustCompile(`(?i)\W+(cam|camrip|hdcam)\W*`):   RipCam,
		regexp.MustCompile(`(?i)\W+(ts|telesync)\W*`):        RipTS,
		regexp.MustCompile(`(?i)\W+(tc|telecine)\W*`):        RipTC,
		regexp.MustCompile(`(?i)\W+(scr|screener)\W*`):       RipScr,
		regexp.MustCompile(`(?i)\W+dvd\W*scr\W*`):            RipDVDScr,
		regexp.MustCompile(`(?i)\W+dvd\W*rip\W*`):            RipDVD,
		regexp.MustCompile(`(?i)\W+hd(tv|rip)\W*`):           RipHDTV,
		regexp.MustCompile(`(?i)\W+(web\W*dl|web\W*rip)\W*`): RipWeb,
		regexp.MustCompile(`(?i)\W+(bluray|b[rd]rip)\W*`):    RipBluRay,
	}
	// Rips ...
	Rips = []string{"", "Cam", "TeleSync", "TeleCine", "Screener", "DVD Screener", "DVDRip", "HDTV", "WebDL", "Blu-Ray"}
)

const (
	// RatingUnkown ...
	RatingUnkown = iota
	// RatingProper ...
	RatingProper
	// RatingNuked ...
	RatingNuked
)

var (
	sceneTags = map[*regexp.Regexp]int{
		regexp.MustCompile(`(?i)\W+nuked\W*`):  RatingNuked,
		regexp.MustCompile(`(?i)\W+proper\W*`): RatingProper,
	}
)

const (
	// CodecUnknown ...
	CodecUnknown = iota

	// CodecXVid ...
	CodecXVid
	// CodecH264 ...
	CodecH264
	// CodecH265 ...
	CodecH265

	// CodecMp3 ...
	CodecMp3
	// CodecAAC ...
	CodecAAC
	// CodecAC3 ...
	CodecAC3
	// CodecDTS ...
	CodecDTS
	// CodecDTSHD ...
	CodecDTSHD
	// CodecDTSHDMA ...
	CodecDTSHDMA
)

var (
	videoTags = map[*regexp.Regexp]int{
		regexp.MustCompile(`(?i)\W+xvid\W*`):           CodecXVid,
		regexp.MustCompile(`(?i)\W+([hx]264)\W*`):      CodecH264,
		regexp.MustCompile(`(?i)\W+([hx]265|hevc)\W*`): CodecH265,
	}
	audioTags = map[*regexp.Regexp]int{
		regexp.MustCompile(`(?i)\W+mp3\W*`):              CodecMp3,
		regexp.MustCompile(`(?i)\W+aac\W*`):              CodecAAC,
		regexp.MustCompile(`(?i)\W+(ac3|[Dd]*5\W+1)\W*`): CodecAC3,
		regexp.MustCompile(`(?i)\W+dts\W*`):              CodecDTS,
		regexp.MustCompile(`(?i)\W+dts\W+hd\W*`):         CodecDTSHD,
		regexp.MustCompile(`(?i)\W+dts\W+hd\W+ma\W*`):    CodecDTSHDMA,
	}
	// Codecs ...
	Codecs = []string{"", "Xvid", "H.264", "H.265", "MP3", "AAC", "AC3", "DTS", "DTS HD", "DTS HD MA"}
)

const (
	xtPrefix = "urn:btih:"
	torCache = "http://itorrents.org/torrent/%s.torrent"
)

// UnmarshalJSON ...
func (t *TorrentFile) UnmarshalJSON(b []byte) error {
	tmp := torrent{}
	if err := json.Unmarshal(b, &tmp); err != nil {
		return err
	}
	*t = TorrentFile(tmp)
	t.initialize()
	return nil
}

// MarshalJSON ...
func (t *TorrentFile) MarshalJSON() ([]byte, error) {
	tmp := torrent(*t)
	b, err := json.Marshal(tmp)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// IsMagnet ...
func (t *TorrentFile) IsMagnet() bool {
	return strings.HasPrefix(t.URI, "magnet:")
}

// IsValidMagnet Taken from anacrolix/torrent
func (t *TorrentFile) IsValidMagnet() (err error) {
	u, err := url.Parse(t.URI)
	if err != nil {
		err = fmt.Errorf("error parsing uri: %s", err)
		return
	}
	if u.Scheme != "magnet" {
		err = fmt.Errorf("unexpected scheme: %q", u.Scheme)
		return
	}
	xt := u.Query().Get("xt")
	if !strings.HasPrefix(xt, xtPrefix) {
		err = fmt.Errorf("bad xt parameter")
		return
	}
	infoHash := xt[len(xtPrefix):]

	// BTIH hash can be in HEX or BASE32 encoding
	// will assign appropriate func judging from symbol length
	var decode func(dst, src []byte) (int, error)
	switch len(infoHash) {
	case 40:
		decode = hex.Decode
	case 32:
		decode = base32.StdEncoding.Decode
	}

	if decode == nil {
		err = fmt.Errorf("unhandled xt parameter encoding: encoded length %d", len(infoHash))
		return
	}
	n, err := decode([]byte(t.InfoHash)[:], []byte(infoHash))
	if err != nil {
		err = fmt.Errorf("error decoding xt: %s", err)
		return
	}
	if n != 20 {
		err = fmt.Errorf("invalid magnet length: %d", n)
		return
	}
	return
}

// NewTorrentFile ...
func NewTorrentFile(uri string) *TorrentFile {
	t := &TorrentFile{
		URI: uri,
	}
	t.initialize()
	return t
}

func (t *TorrentFile) initialize() {
	if t.IsMagnet() {
		t.initializeFromMagnet()
	}

	if t.Resolution == ResolutionUnknown {
		t.Resolution = matchLowerTags(t, resolutionTags)
		if t.Resolution == ResolutionUnknown {
			t.Resolution = Resolution480p
		}
	}
	if t.VideoCodec == CodecUnknown {
		t.VideoCodec = matchTags(t, videoTags)
	}
	if t.AudioCodec == CodecUnknown {
		t.AudioCodec = matchTags(t, audioTags)
	}
	if t.RipType == RipUnknown {
		t.RipType = matchTags(t, ripTags)
	}
	if t.SceneRating == RatingUnkown {
		t.SceneRating = matchTags(t, sceneTags)
	}
	t.beautifySize()
	t.parseSize()
}

func (t *TorrentFile) initializeFromMagnet() {
	magnetURI, _ := url.Parse(t.URI)
	vals := magnetURI.Query()
	hash := strings.ToUpper(strings.TrimPrefix(vals.Get("xt"), "urn:btih:"))

	// for backward compatibility
	if unBase32Hash, err := base32.StdEncoding.DecodeString(hash); err == nil {
		hash = hex.EncodeToString(unBase32Hash)
	}

	if t.InfoHash == "" {
		t.InfoHash = strings.ToLower(hash)
	}
	if t.Name == "" {
		t.Name = vals.Get("dn")
	}
	if t.Title == "" {
		t.Title = t.Name
	}

	if len(t.Trackers) == 0 {
		t.Trackers = make([]string, 0)
		for _, tracker := range vals["tr"] {
			t.Trackers = append(t.Trackers, strings.Replace(string(tracker), "\\", "", -1))
		}
	}
}

// Magnet ...
func (t *TorrentFile) Magnet() {
	if t.hasResolved == false {
		t.Resolve()
	}

	params := url.Values{}
	params.Set("dn", t.Name)
	if config.Get().MagnetTrackers != magnetEnricherClear {
		if len(t.Trackers) != 0 {
			for _, tracker := range t.Trackers {
				params.Add("tr", tracker)
			}
		}
	}
	if config.Get().MagnetTrackers == magnetEnricherAdd {
		for _, tracker := range DefaultTrackers {
			if !util.StringSliceContains(t.Trackers, tracker) {
				params.Add("tr", tracker)
			}
		}
	}

	t.URI = fmt.Sprintf("magnet:?xt=urn:btih:%s&%s", t.InfoHash, params.Encode())

	if t.IsValidMagnet() == nil {
		params.Add("as", t.URI)
	} else {
		params.Add("as", fmt.Sprintf(torCache, t.InfoHash))
	}
}

// LoadFromBytes ...
func (t *TorrentFile) LoadFromBytes(in []byte) error {

	var torrentFile *TorrentFileRaw

	if err := bencode.DecodeBytes(in, &torrentFile); err != nil {
		return err
	}

	if t.InfoHash == "" {
		hasher := sha1.New()
		bencode.NewEncoder(hasher).Encode(torrentFile.Info)
		t.InfoHash = hex.EncodeToString(hasher.Sum(nil))
	}

	if t.Name == "" {
		t.Name = torrentFile.Info["name"].(string)
	}
	if t.Title == "" {
		if len(torrentFile.Title) > 0 {
			t.Title = torrentFile.Title
		} else {
			t.Title = t.Name
		}
	}

	if torrentFile.Info["private"] != nil {
		if torrentFile.Info["private"].(int64) == 1 {
			// torrentFileLog.Noticef("%s marked as private", t.Name)
			t.IsPrivate = true
		}
	}

	if len(t.Trackers) == 0 {
		t.Trackers = append(t.Trackers, torrentFile.Announce)
		for _, trackers := range torrentFile.AnnounceList {
			t.Trackers = append(t.Trackers, trackers...)
		}
	}

	// Save torrent file in temp folder
	torrentFileName := filepath.Join(config.Get().Info.TempPath, fmt.Sprintf("%s.torrent", t.InfoHash))
	out, err := os.Create(torrentFileName)
	if err != nil {
		return err
	}
	defer func() {
		cerr := out.Close()
		if err == nil {
			err = cerr
		}
	}()
	buf := bytes.NewReader(in)
	if _, err := io.Copy(out, buf); err != nil {
		return err
	}
	t.URI = torrentFileName

	t.hasResolved = true

	t.initialize()

	return nil
}

// Resolve ...
func (t *TorrentFile) Resolve() error {
	if t.IsMagnet() {
		t.hasResolved = true
		return nil
	}

	parts := strings.Split(t.URI, "|")
	uri := parts[0]
	req, err := http.NewRequest("GET", uri, nil)
	if err != nil {
		return err
	}
	if len(parts) > 1 {
		for _, part := range parts[1:] {
			keyVal := strings.SplitN(part, "=", 2)
			req.Header.Add(keyVal[0], keyVal[1])
		}
	}

	resp, err := scrape.GetClient().Do(req)
	if err != nil {
		return err
	} else if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Request %s failed with code: %d", uri, resp.StatusCode)
	}
	defer resp.Body.Close()

	var reader io.ReadCloser
	switch resp.Header.Get("Content-Encoding") {
	case "gzip":
		reader, err = gzip.NewReader(resp.Body)
		defer reader.Close()
	default:
		reader = resp.Body
	}

	var buf bytes.Buffer
	var torrentFile *TorrentFileRaw

	tee := io.TeeReader(reader, &buf)
	dec := bencode.NewDecoder(tee)

	if errDec := dec.Decode(&torrentFile); errDec != nil {
		return fmt.Errorf("Decode error: %s", errDec)
	}

	if t.InfoHash == "" {
		hasher := sha1.New()
		bencode.NewEncoder(hasher).Encode(torrentFile.Info)
		t.InfoHash = hex.EncodeToString(hasher.Sum(nil))
	}

	if t.Name == "" {
		t.Name = torrentFile.Info["name"].(string)
	}
	if t.Title == "" {
		if len(torrentFile.Title) > 0 {
			t.Title = torrentFile.Title
		} else {
			t.Title = t.Name
		}
	}

	if torrentFile.Info["private"] != nil {
		if torrentFile.Info["private"].(int64) == 1 {
			t.IsPrivate = true
		}
	}

	if len(t.Trackers) == 0 {
		t.Trackers = append(t.Trackers, torrentFile.Announce)
		for _, trackers := range torrentFile.AnnounceList {
			t.Trackers = append(t.Trackers, trackers...)
		}
	}

	// Save torrent file in temp folder
	torrentFileName := filepath.Join(config.Get().Info.TempPath, fmt.Sprintf("%s.torrent", t.InfoHash))
	out, err := os.Create(torrentFileName)
	if err != nil {
		return err
	}
	defer func() {
		cerr := out.Close()
		if err == nil {
			err = cerr
		}
	}()
	if _, err := io.Copy(out, &buf); err != nil {
		return err
	}
	t.URI = torrentFileName

	t.hasResolved = true

	t.initialize()

	return nil
}

func matchTags(t *TorrentFile, tokens map[*regexp.Regexp]int) int {
	lowName := strings.ToLower(t.Name)
	codec := 0
	for re, value := range tokens {
		if re.MatchString(lowName) {
			if value > codec {
				codec = value
			}
		}
	}
	return codec
}

func matchLowerTags(t *TorrentFile, tokens []map[*regexp.Regexp]int) int {
	lowName := strings.ToLower(t.Name)
	for _, res := range tokens {
		for re, value := range res {
			if re.MatchString(lowName) {
				return value
			}
		}
	}
	return 0
}

// StreamInfo ...
func (t *TorrentFile) StreamInfo() *xbmc.StreamInfo {
	sie := &xbmc.StreamInfo{
		Video: &xbmc.StreamInfoEntry{
			Codec: Codecs[t.VideoCodec],
		},
		Audio: &xbmc.StreamInfoEntry{
			Codec: Codecs[t.AudioCodec],
		},
	}

	switch t.Resolution {
	case Resolution480p:
		sie.Video.Width = 853
		sie.Video.Height = 480
		break
	case Resolution720p:
		sie.Video.Width = 1280
		sie.Video.Height = 720
		break
	case Resolution1080p:
		sie.Video.Width = 1920
		sie.Video.Height = 1080
		break
	case Resolution2K:
		sie.Video.Width = 2560
		sie.Video.Height = 1440
		break
	case Resolution4k:
		sie.Video.Width = 4096
		sie.Video.Height = 2160
		break
	}

	return sie
}

func (t *TorrentFile) beautifySize() {
	// To upper-case
	t.Size = strings.ToUpper(t.Size)

	// Cyrillic-specific translation
	t.Size = strings.Replace(t.Size, "КБ", "KB", -1)
	t.Size = strings.Replace(t.Size, "МБ", "MB", -1)
	t.Size = strings.Replace(t.Size, "ГБ", "GB", -1)
	t.Size = strings.Replace(t.Size, "ТБ", "TB", -1)
	t.Size = strings.Replace(t.Size, "MO", "MB", -1)
	t.Size = strings.Replace(t.Size, "GO", "GB", -1)

	// Replacing "," with "."
	t.Size = strings.Replace(t.Size, ",", ".", -1)

	// Adding whitespace after number
	t.Size = sizeMatcher.ReplaceAllString(t.Size, "$1 ")
}

func (t *TorrentFile) parseSize() {
	if len(t.Size) == 0 {
		return
	}

	if v, err := humanize.ParseBytes(t.Size); err == nil {
		t.SizeParsed = v
	} else {
		log.Debugf("Could not parse torrent size for '%s': %#v", t.Size, err)
	}
}
