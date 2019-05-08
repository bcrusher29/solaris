package main

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/bcrusher29/solaris/bittorrent"
	"github.com/bcrusher29/solaris/config"
	"github.com/bcrusher29/solaris/database"
	"github.com/bcrusher29/solaris/repository"
	"github.com/bcrusher29/solaris/xbmc"
)

func checkRepository() bool {
	if xbmc.IsAddonInstalled("repository.elementum") {
		if !xbmc.IsAddonEnabled("repository.elementum") {
			xbmc.SetAddonEnabled("repository.elementum", true)
		}
		return true
	}

	log.Info("Creating Elementum repository add-on...")
	if err := repository.MakeElementumRepositoryAddon(); err != nil {
		log.Errorf("Unable to create repository add-on: %s", err)
		return false
	}

	xbmc.UpdateLocalAddons()
	for _, addon := range xbmc.GetAddons("xbmc.addon.repository", "unknown", "all", []string{"name", "version", "enabled"}).Addons {
		if addon.ID == "repository.elementum" && addon.Enabled == true {
			log.Info("Found enabled Elementum repository add-on")
			return false
		}
	}
	log.Info("Elementum repository not installed, installing...")
	xbmc.InstallAddon("repository.elementum")
	xbmc.SetAddonEnabled("repository.elementum", true)
	xbmc.UpdateLocalAddons()
	xbmc.UpdateAddonRepos()

	return true
}

func migrateDB() bool {
	firstRun := filepath.Join(config.Get().Info.Profile, ".dbfirstrun")
	if _, err := os.Stat(firstRun); err == nil {
		return false
	}
	file, _ := os.Create(firstRun)
	defer file.Close()

	log.Info("Migrating old bolt DB to Sqlite ...")
	defer func() {
		log.Info("... migration finished")
	}()

	newDB := database.Get()
	oldDB, err := database.NewBoltDB()
	if err != nil {
		return false
	}

	for _, t := range []string{"", "movies", "shows"} {
		list := []string{}
		if err := oldDB.GetObject(database.HistoryBucket, "list"+t, &list); err != nil {
			continue
		}
		for i := len(list) - 1; i >= 0; i-- {
			newDB.AddSearchHistory(t, list[i])
		}

		oldDB.Delete(database.HistoryBucket, "list"+t)
	}

	oldDB.Seek(database.TorrentHistoryBucket, "", func(k, v []byte) {
		_, err := strconv.Atoi(string(k))
		if err != nil || len(v) <= 0 {
			return
		}

		torrent := &bittorrent.TorrentFile{}
		err = torrent.LoadFromBytes(v)
		if err != nil || len(v) <= 0 || torrent.InfoHash == "" {
			return
		}

		newDB.AddTorrentLink(string(k), torrent.InfoHash, v)
		oldDB.Delete(database.TorrentHistoryBucket, string(k))
	})
	return true
}
