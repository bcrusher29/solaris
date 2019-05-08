package database

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	// Importing sqlite driver
	_ "github.com/mattn/go-sqlite3"

	"github.com/bcrusher29/solaris/config"
	"github.com/bcrusher29/solaris/util"
)

// InitSqliteDB ...
func InitSqliteDB(conf *config.Configuration) (*SqliteDatabase, error) {
	db, err := CreateSqliteDB(conf, sqliteFileName, backupSqliteFileName)
	if err != nil || db == nil {
		return nil, errors.New("database not created")
	}

	sqliteDatabase = &SqliteDatabase{
		DB:             db,
		quit:           make(chan struct{}, 2),
		fileName:       sqliteFileName,
		backupFileName: backupSqliteFileName,
	}

	// Iterate through changesets and apply to the database
	currentVersion := sqliteDatabase.getSchemaVersion()
	newVersion := currentVersion
	for _, s := range schemaChanges {
		if ok, err := s(&newVersion, sqliteDatabase); err != nil {
			log.Errorf("Error migrating schema: %s", err)
			break
		} else if ok {
			continue
		}

		break
	}
	if newVersion > currentVersion {
		log.Debugf("Updated database to version %d from version %d", newVersion, currentVersion)
		sqliteDatabase.setSchemaVersion(newVersion)
	}

	return sqliteDatabase, nil
}

// CreateSqliteDB ...
func CreateSqliteDB(conf *config.Configuration, fileName string, backupFileName string) (*sql.DB, error) {
	databasePath := filepath.Join(conf.Info.Profile, fileName)
	backupPath := filepath.Join(conf.Info.Profile, backupFileName)

	defer func() {
		if r := recover(); r != nil {
			log.Errorf("Got critical error while creating SQLite: %v", r)
			RestoreBackup(databasePath, backupPath)
			os.Exit(1)
		}
	}()

	db, err := sql.Open("sqlite3", databasePath)
	if err != nil {
		log.Warningf("Could not open database at %s: %s", databasePath, err.Error())
		return nil, err
	}

	// Setting up default properties for connection
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA foreign_keys=ON")
	db.SetMaxOpenConns(1)

	return db, nil
}

// Get returns sqlite database
func Get() *SqliteDatabase {
	return sqliteDatabase
}

// GetFilename returns sqlite filename
func (d *SqliteDatabase) GetFilename() string {
	return d.fileName
}

// MaintenanceRefreshHandler ...
func (d *SqliteDatabase) MaintenanceRefreshHandler() {
	backupPath := filepath.Join(config.Get().Info.Profile, d.backupFileName)

	d.CreateBackup(backupPath)

	// database.CacheCleanup()

	tickerBackup := time.NewTicker(1 * time.Hour)
	defer tickerBackup.Stop()

	defer close(d.quit)

	for {
		select {
		case <-tickerBackup.C:
			go func() {
				d.CreateBackup(backupPath)
			}()
			// case <-tickerCache.C:
			// 	go database.CacheCleanup()
		case <-d.quit:
			return
		}
	}
}

// CreateBackup ...
func (d *SqliteDatabase) CreateBackup(backupPath string) {
	src := filepath.Join(config.Get().Info.Profile, d.fileName)
	dest := filepath.Join(config.Get().Info.Profile, d.backupFileName)
	if err := util.CopyFile(src, dest, true); err != nil {
		log.Warningf("Could not backup from '%s' to '%s' the database: %s", src, dest, err)
	}
}

// Close ...
func (d *SqliteDatabase) Close() {
	log.Debug("Closing Sqlite Database")
	d.quit <- struct{}{}
	d.DB.Close()
}

// GetSetting ...
func (d *SqliteDatabase) GetSetting(name string) (value string) {
	d.DB.QueryRow(`SELECT value FROM settings WHERE name = ?`, name).Scan(&value)
	return
}

// SetSetting ...
func (d *SqliteDatabase) SetSetting(name, value string) {
	if _, err := d.DB.Exec(`INSERT OR REPLACE INTO settings (name, value) VALUES (?, ?)`, name, value); err != nil {
		log.Debugf("Could not update setting (%s=%s): %s", name, value, err)
	}
}

func (d *SqliteDatabase) getSchemaVersion() (version int) {
	v := d.GetSetting("version")
	version, _ = strconv.Atoi(v)
	return
}

func (d *SqliteDatabase) setSchemaVersion(version int) {
	v := strconv.Itoa(version)
	d.SetSetting("version", v)
}

// GetCount is a helper for returning single column int result
func (d *SqliteDatabase) GetCount(sql string) (count int) {
	_ = d.DB.QueryRow(sql).Scan(&count)
	return
}

// AddSearchHistory adds query to search history, according to media type
func (d *SqliteDatabase) AddSearchHistory(historyType, query string) {
	rowid := 0
	d.QueryRow(`SELECT rowid FROM history_queries WHERE type = ? AND query = ?`, historyType, query).Scan(&rowid)

	if rowid > 0 {
		d.Exec(`UPDATE history_queries SET dt = ? WHERE rowid = ?`, time.Now().Unix(), rowid)
		return
	}

	if _, err := d.Exec(`INSERT INTO history_queries (type, query, dt) VALUES(?, ?, ?)`, historyType, query, time.Now().Unix()); err != nil {
		return
	}

	d.Exec(`DELETE FROM history_queries WHERE type = ? AND rowid NOT IN (SELECT rowid FROM history_queries WHERE type = ? ORDER BY dt DESC LIMIT ?)`, historyType, historyType, historyMaxSize)
}

// AddTorrentLink saves link between torrent file and tmdbID entry
func (d *SqliteDatabase) AddTorrentLink(tmdbID, infoHash string, b []byte) {
	log.Debugf("Saving torrent entry for TMDB %s with infohash %s", tmdbID, infoHash)

	var infohashID int64
	d.QueryRow(`SELECT rowid FROM thistory_metainfo WHERE infohash = ?`, infoHash).Scan(&infohashID)
	if infohashID == 0 {
		res, err := d.Exec(`INSERT INTO thistory_metainfo (infohash, metainfo) VALUES(?, ?)`, infoHash, b)
		if err != nil {
			log.Debugf("Could not insert torrent: %s", err)
			return
		}
		infohashID, err = res.LastInsertId()
		if err != nil {
			log.Debugf("Could not get inserted rowid: %s", err)
			return
		}
	}

	if infohashID == 0 {
		return
	}

	var oldInfohashID int64
	d.QueryRow(`SELECT infohash_id FROM thistory_assign WHERE item_id = ?`, tmdbID).Scan(&oldInfohashID)
	d.Exec(`INSERT OR REPLACE INTO thistory_assign (infohash_id, item_id) VALUES (?, ?)`, infohashID, tmdbID)

	// Clean up previously saved torrent metainfo if there is any
	// and it's not used by any other tmdbID entry
	if oldInfohashID != 0 {
		var left int
		d.QueryRow(`SELECT COUNT(*) FROM thistory_assign WHERE infohash_id = ?`, oldInfohashID).Scan(&left)
		if left == 0 {
			d.Exec(`DELETE FROM thistory_metainfo WHERE rowid = ?`, oldInfohashID)
		}
	}
}

// Bittorrent Database handlers

// GetBTItem ...
func (d *SqliteDatabase) GetBTItem(infoHash string) *BTItem {
	item := &BTItem{}
	rowid := 0
	fileStr := ""
	infoStr := ""

	d.QueryRow(`SELECT rowid, state, mediaID, mediaType, files, infos FROM tinfo WHERE infohash = ?`, infoHash).Scan(&rowid, &item.State, &item.ID, &item.Type, &fileStr, &infoStr)
	if rowid == 0 {
		return nil
	}

	item.Files = strings.Split(fileStr, "|")
	infos := strings.Split(infoStr, "|")
	if len(infos) >= 3 {
		item.ShowID, _ = strconv.Atoi(infos[0])
		item.Season, _ = strconv.Atoi(infos[1])
		item.Episode, _ = strconv.Atoi(infos[2])
	}
	if len(infos) >= 4 {
		item.Query = infos[3]
	}

	return item
}

// UpdateBTItemStatus ...
func (d *SqliteDatabase) UpdateBTItemStatus(infoHash string, status int) error {
	_, err := d.Exec(`UPDATE tinfo SET state = ? WHERE infohash = ?`, status, infoHash)
	return err
}

// UpdateBTItem ...
func (d *SqliteDatabase) UpdateBTItem(infoHash string, mediaID int, mediaType string, files []string, query string, infos ...int) error {
	fileStr := ""
	for _, f := range files {
		if f != "" {
			if len(fileStr) > 0 {
				fileStr += "|"
			}

			fileStr += f
		}
	}

	infoStr := ""
	for _, f := range infos {
		if len(infoStr) > 0 {
			infoStr += "|"
		}
		infoStr += strconv.Itoa(f)
	}
	if len(infoStr) > 0 {
		infoStr += "|"
	}
	infoStr += query

	_, err := d.Exec(`INSERT OR REPLACE INTO tinfo (infohash, state, mediaID, mediaType, files, infos) VALUES (?, ?, ?, ?, ?, ?)`, infoHash, StatusActive, mediaID, mediaType, fileStr, infoStr)
	if err != nil {
		log.Debugf("UpdateBTItem failed: %s", err)
	}
	return err
}

// UpdateBTItemFiles ...
func (d *SqliteDatabase) UpdateBTItemFiles(infoHash string, files []string) error {
	fileStr := ""
	for _, f := range files {
		if f != "" {
			if len(fileStr) > 0 {
				fileStr += "|"
			}

			fileStr += f
		}
	}

	_, err := d.Exec(`UPDATE tinfo SET files = ? WHERE infohash = ?`, fileStr, infoHash)
	if err != nil {
		log.Debugf("UpdateBTItemFiles failed: %s", err)
	}
	return err
}

// DeleteBTItem ...
func (d *SqliteDatabase) DeleteBTItem(infoHash string) error {
	_, err := d.Exec(`DELETE FROM tinfo WHERE infohash = ?`, infoHash)
	return err
}

// AddTorrentHistory saves last used torrent
func (d *SqliteDatabase) AddTorrentHistory(infoHash, name string, b []byte) {
	if !config.Get().UseTorrentHistory {
		return
	}

	log.Debugf("Saving torrent %s with infohash %s to the history", name, infoHash)

	rowid := 0
	d.QueryRow(`SELECT rowid FROM torrent_history WHERE infohash = ?`, infoHash).Scan(&rowid)

	if rowid > 0 {
		d.Exec(`UPDATE torrent_history SET dt = ? WHERE rowid = ?`, time.Now().Unix(), rowid)
		return
	}

	if _, err := d.Exec(`INSERT INTO torrent_history (infohash, name, dt, metainfo) VALUES(?, ?, ?, ?)`, infoHash, name, time.Now().Unix(), b); err != nil {
		log.Warningf("Error inserting item to the history: %s", err)
		return
	}

	d.Exec(`DELETE FROM torrent_history WHERE rowid NOT IN (SELECT rowid FROM torrent_history ORDER BY dt DESC LIMIT ?)`, config.Get().TorrentHistorySize)
}
