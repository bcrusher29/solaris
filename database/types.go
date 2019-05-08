package database

import (
	"database/sql"
	"sync"

	"github.com/boltdb/bolt"
	"github.com/op/go-logging"
)

// BoltDatabase ...
type BoltDatabase struct {
	db             *bolt.DB
	quit           chan struct{}
	fileName       string
	backupFileName string
}

// SqliteDatabase ...
type SqliteDatabase struct {
	*sql.DB
	quit           chan struct{}
	fileName       string
	backupFileName string
}

type schemaChange func(*int, *SqliteDatabase) (bool, error)

type callBack func([]byte, []byte)
type callBackWithError func([]byte, []byte) error

// DBWriter ...
type DBWriter struct {
	bucket   []byte
	key      []byte
	database *BoltDatabase
}

// BTItem ...
type BTItem struct {
	ID      int      `json:"id"`
	State   int      `json:"state"`
	Type    string   `json:"type"`
	Files   []string `json:"files"`
	ShowID  int      `json:"showid"`
	Season  int      `json:"season"`
	Episode int      `json:"episode"`
	Query   string   `json:"query"`
}

var (
	sqliteFileName       = "app.db"
	backupSqliteFileName = "app-backup.db"
	boltFileName         = "library.db"
	backupBoltFileName   = "library-backup.db"
	cacheFileName        = "cache.db"
	backupCacheFileName  = "cache-backup.db"

	log = logging.MustGetLogger("database")

	sqliteDatabase *SqliteDatabase
	boltDatabase   *BoltDatabase
	cacheDatabase  *BoltDatabase

	once sync.Once
)

const (
	// StatusRemove ...
	StatusRemove = iota
	// StatusActive ...
	StatusActive
)

const (
	historyMaxSize = 50
)
