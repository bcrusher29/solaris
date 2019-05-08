package quasar

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bcrusher29/solaris/config"
	"github.com/bcrusher29/solaris/database"
	"github.com/bcrusher29/solaris/xbmc"

	"github.com/boltdb/bolt"
	"github.com/karrick/godirwalk"
	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("migrate")

const (
	movie = iota
	show
	season
	episode
	removedMovie
	removedShow
	removedSeason
	removedEpisode
)

type migrateItem struct {
	ID       string `json:"id"`
	Type     int    `json:"type"`
	TVShowID int    `json:"showid"`
}

// Migrate makes library migration from Quasar to Elementum
func Migrate() (err error) {
	if !xbmc.DialogConfirm("Elementum", "LOCALIZE[30337]") {
		log.Debugf("User cancelled Quasar migration")
		return
	}

	log.Debugf("Starting Quasar migration")

	// Prepare Dialog instance
	var dialogProgress *xbmc.DialogProgressBG
	if !config.Get().DisableBgProgress {
		dialogProgress = xbmc.NewDialogProgressBG("Elementum", "LOCALIZE[30338]", "LOCALIZE[30338]", "LOCALIZE[30339]", "LOCALIZE[30340]", "LOCALIZE[30341]")
	}
	defer func() {
		if !config.Get().DisableBgProgress && dialogProgress != nil {
			dialogProgress.Close()
		}
	}()

	// Step 1: Find Quasar
	progressBase := 0
	progressStep := 0
	if !config.Get().DisableBgProgress {
		dialogProgress.Update(progressBase+progressStep, "Elementum", "LOCALIZE[30338]")
	}
	pluginUserdata := filepath.Join(config.Get().Info.Profile, "..", "plugin.video.quasar")
	if _, errOS := os.Stat(pluginUserdata); errOS != nil {
		log.Debugf("Quasar directory not found (%#v): %#v", pluginUserdata, errOS)
		xbmc.Dialog("Elementum", "LOCALIZE[30342]")
		return errOS
	}
	log.Debugf("Using Quasar userdata directory: %#v", pluginUserdata)

	pluginSettings := filepath.Join(pluginUserdata, "settings.xml")
	pluginDatabase := filepath.Join(pluginUserdata, "library.db")
	if _, errOS := os.Stat(pluginSettings); errOS != nil {
		log.Debugf("Quasar settings not found (%#v): %#v", pluginSettings, errOS)
		xbmc.Dialog("Elementum", "LOCALIZE[30343]")
		return errOS
	}
	log.Debugf("Using Quasar settings file: %#v", pluginSettings)
	log.Debugf("Using Quasar database file: %#v", pluginDatabase)

	// Disabling Quasar plugin if active, otherwise we can't edit it's database
	for _, addon := range xbmc.GetAddons("xbmc.addon.video", "unknown", "all", []string{"name", "version", "enabled"}).Addons {
		if addon.ID == "plugin.video.quasar" && addon.Enabled {
			log.Debugf("Disabling Quasar plugin...")
			xbmc.SetAddonEnabled("plugin.video.quasar", false)
			time.Sleep(5 * time.Second)
		}
	}

	// Step 2: Find strm files
	progressBase = 25
	progressStep = 0
	if !config.Get().DisableBgProgress {
		dialogProgress.Update(progressBase+progressStep, "Elementum", "LOCALIZE[30339]")
	}

	sourceDirectories := []string{}
	sourceFiles := map[string]bool{}

	kodiSources := xbmc.FilesGetSources()
	log.Debugf("Kodi FilesGetSources: %#v", kodiSources)
	for _, source := range kodiSources.Sources {
		sourceDirectories = append(sourceDirectories, xbmc.TranslatePath(source.FilePath))
	}

	pluginLibrary := ""
	settingsContent, _ := ioutil.ReadFile(pluginSettings)
	if len(settingsContent) > 0 {
		r := regexp.MustCompile(`(?m:id="library_path" value="(.*?)")`)
		matches := r.FindSubmatch(settingsContent)
		if len(matches) >= 1 {
			pluginLibrary = string(matches[1])
		}
	}
	if len(pluginLibrary) == 0 {
		log.Debugf("Quasar library not found in %#v", pluginSettings)
		xbmc.Dialog("Elementum", "LOCALIZE[30344]")
		return errors.New("Quasar library not found")
	}
	log.Debugf("Using Quasar library directory: %#v", pluginLibrary)

	if len(pluginLibrary) > 0 {
		sourceDirectories = append(sourceDirectories, xbmc.TranslatePath(pluginLibrary))
	}

	for i, source := range sourceDirectories {
		log.Debugf("Processing the source: %s", source)
		files := searchStrm(source)
		if len(files) != 0 {
			for _, f := range files {
				sourceFiles[f] = true
			}
		}

		progressStep = int(float64(i+1) / float64(len(sourceDirectories)) * 25)
		if !config.Get().DisableBgProgress {
			dialogProgress.Update(progressBase+progressStep, "Elementum", "LOCALIZE[30339]")
		}
	}
	log.Debugf("Prepared strm files: %#v", len(sourceFiles))

	// Step 3: Migrating strm files
	progressBase = 50
	progressStep = 0
	if !config.Get().DisableBgProgress {
		dialogProgress.Update(progressBase+progressStep, "Elementum", "LOCALIZE[30339]")
	}

	// Possible urls:
	// /movie/:tmdbId/links
	// /movie/:tmdbId/play

	// /show/:showId/season/:season/links
	// /show/:showId/season/:season/episode/:episode/play
	// /show/:showId/season/:season/episode/:episode/links

	// /library/movie/play/:tmdbId
	// /library/show/play/:showId/:season/:episode
	// /library/play/movie/:tmdbId
	// /library/play/show/:showId/season/:season/episode/:episode

	rMovies := []*regexp.Regexp{
		regexp.MustCompile(`(?m:/movie/(\d+)/\w+)`),
		regexp.MustCompile(`(?m:/library/movie/play/(\d+))`),
		regexp.MustCompile(`(?m:/library/play/movie/(\d+))`),
	}
	rShows := []*regexp.Regexp{
		regexp.MustCompile(`(?m:/show/(\d+)/season/:season/links)`),
		regexp.MustCompile(`(?m:/show/(\d+)/season/(\d+)/episode/(\d+)/\w+)`),
		regexp.MustCompile(`(?m:/library/show/play/(\d+)/(\d+)/(\d+))`),
		regexp.MustCompile(`(?m:/library/play/show/(\d+)/season/(\d+)/episode/(\d+))`),
	}

	migrateItems := map[int]migrateItem{}
	fileCounter := 0
	for filePath := range sourceFiles {
		fileContent, _ := ioutil.ReadFile(filePath)
		if len(fileContent) > 0 {
			if out := strings.Replace(string(fileContent), "plugin.video.quasar", "plugin.video.elementum", -1); len(out) != len(fileContent) {
				ioutil.WriteFile(filePath, []byte(out), 0644)
			} else {
				continue
			}

			mType := -1
			mTMDB := 0

			for _, r := range rMovies {
				if matches := r.FindSubmatch(fileContent); len(matches) > 0 {
					mType = movie
					mTMDB, _ = strconv.Atoi(string(matches[1]))
				}
			}
			for _, r := range rShows {
				if matches := r.FindSubmatch(fileContent); len(matches) > 0 {
					mType = show
					mTMDB, _ = strconv.Atoi(string(matches[1]))
				}
			}

			if mTMDB != 0 && mType != -1 {
				migrateItems[mTMDB] = migrateItem{
					ID:   strconv.Itoa(mTMDB),
					Type: mType,
				}
			} else {
				log.Debugf("Not Matched in %#v ", string(fileContent))
			}
		}

		fileCounter++
		if fileCounter%100 == 0 {
			progressStep = int(float64(fileCounter) / float64(len(sourceFiles)) * 25)
			if !config.Get().DisableBgProgress {
				dialogProgress.Update(progressBase+progressStep, "Elementum", "LOCALIZE[30339]")
			}
		}
	}

	// Step 4: Migrating database
	progressBase = 75
	progressStep = 0
	if !config.Get().DisableBgProgress {
		dialogProgress.Update(progressBase+progressStep, "Elementum", "LOCALIZE[30339]")
	}

	migratedCounter := 0
	bucket := []byte("Library")
	newDB, _ := database.NewBoltDB()
	oldDB, errDB := bolt.Open(pluginDatabase, 0600, &bolt.Options{
		ReadOnly: false,
		Timeout:  15 * time.Second,
	})
	if errDB != nil {
		log.Debugf("Could not open database at %s: %s", pluginDatabase, errDB.Error())
		return errDB
	}
	oldDB.NoSync = true

	log.Debugf("Migrating %#v items", len(migrateItems))
	for _, item := range migrateItems {
		migratedCounter++

		// Add to New library
		newDB.SetObject(bucket, fmt.Sprintf("%d_%s", item.Type, item.ID), item)

		// Remove from Old library
		err = oldDB.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket(bucket)
			if b == nil {
				return fmt.Errorf("Non-existing bucket")
			}
			if errDB := b.Delete([]byte(fmt.Sprintf("%d_%s", item.Type, item.ID))); errDB != nil {
				return errDB
			}
			return nil
		})
		if err != nil {
			continue
		}

		// Add to Deleted into Old library
		err = oldDB.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket(bucket)
			if b == nil {
				return fmt.Errorf("Non-existing bucket")
			}

			Type := removedShow
			if item.Type == movie {
				Type = removedMovie
			}

			if buf, err := json.Marshal(item); err != nil {
				return err
			} else if err := b.Put([]byte(fmt.Sprintf("%d_%s", Type, item.ID)), buf); err != nil {
				return err
			}
			return nil
		})
	}

	oldDB.Close()

	if migratedCounter > 0 {
		xbmc.Dialog("Elementum", fmt.Sprintf("LOCALIZE[30345];;%d", migratedCounter))
	} else {
		xbmc.Dialog("Elementum", "LOCALIZE[30346]")
	}

	log.Debugf("Ended Quasar migration")

	return
}

func searchStrm(dir string) []string {
	ret := []string{}

	godirwalk.Walk(dir, &godirwalk.Options{
		FollowSymbolicLinks: true,
		Callback: func(osPathname string, de *godirwalk.Dirent) error {
			if strings.HasSuffix(osPathname, ".strm") {
				ret = append(ret, osPathname)
			}
			return nil
		},
	})

	return ret
}
