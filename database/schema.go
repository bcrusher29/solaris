package database

var schemaChanges = []schemaChange{
	schemaV1,
	schemaV2,
}

func schemaV1(previousVersion *int, db *SqliteDatabase) (success bool, err error) {
	version := 1

	if *previousVersion > version {
		return
	}

	sql := `

-- Table that stores database specific info, like last rolled version
CREATE TABLE IF NOT EXISTS settings (
  name TEXT NOT NULL UNIQUE,
  value TEXT NOT NULL
);
INSERT OR REPLACE INTO settings (name, value) VALUES ('version', '1');

-- Table for Search queries history
CREATE TABLE IF NOT EXISTS history_queries (
  type INT NOT NULL DEFAULT "",
  query TEXT NOT NULL DEFAULT "",
  dt INT NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS history_queries_idx ON history_queries (type,  dt DESC);

-- Table that stores torrents' metadata
CREATE TABLE IF NOT EXISTS thistory_metainfo (
  infohash TEXT NOT NULL UNIQUE,
  metainfo BLOB
);
CREATE INDEX IF NOT EXISTS thistory_metainfo_idx ON thistory_metainfo (infohash);

-- Table stores links between items and stored metadata
CREATE TABLE IF NOT EXISTS thistory_assign (
  infohash_id INT NOT NULL,
  item_id INT NOT NULL UNIQUE
);
CREATE INDEX IF NOT EXISTS thistory_assign_idx ON thistory_assign (item_id, infohash_id);

-- Table stores torrent information, like TMDB ID and selected Files
CREATE TABLE IF NOT EXISTS tinfo (
  infohash TEXT NOT NULL UNIQUE,
  state INT NOT NULL DEFAULT 0,
  mediaID INT NOT NULL DEFAULT 0,
  mediaType TEXT NOT NULL DEFAULT "",
  files TEXT NOT NULL DEFAULT "",
  infos TEXT NOT NULL DEFAULT ""
);
CREATE INDEX IF NOT EXISTS tinfo_idx ON tinfo (infohash);

-- Table stores library-related items
CREATE TABLE IF NOT EXISTS library_items (
  tmdbId INTEGER NOT NULL UNIQUE,
  state INT NOT NULL DEFAULT 0,
  mediaType INTEGER NOT NULL DEFAULT 0,
  showId INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS library_items_idx1 ON library_items (tmdbId);
CREATE INDEX IF NOT EXISTS library_items_idx2 ON library_items (showId);
CREATE INDEX IF NOT EXISTS library_items_idx3 ON library_items (tmdbId, mediaType, state);
CREATE INDEX IF NOT EXISTS library_items_idx4 ON library_items (mediaType, state);

-- Table stores resolved UIDs
CREATE TABLE IF NOT EXISTS library_uids (
  mediaType INTEGER NOT NULL DEFAULT 0,
  kodi INTEGER NOT NULL DEFAULT 0,
  tmdb INTEGER NOT NULL DEFAULT 0,
  tvdb INTEGER NOT NULL DEFAULT 0,
  trakt INTEGER NOT NULL DEFAULT 0,
  imdb TEXT NOT NULL DEFAULT "",
  playcount INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS library_uids_idx1 ON library_uids (mediaType, kodi);
CREATE INDEX IF NOT EXISTS library_uids_idx2 ON library_uids (mediaType, tmdb);

`

	// Just run an a bunch of statements
	// If everything is fine - return success so we won't get in there again
	if _, err = db.Exec(sql); err == nil {
		*previousVersion = version
		success = true
	}

	return
}

func schemaV2(previousVersion *int, db *SqliteDatabase) (success bool, err error) {
	version := 2

	if *previousVersion > version {
		return
	}

	sql := `

-- Table for Search queries history
CREATE TABLE IF NOT EXISTS torrent_history (
  infohash TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL DEFAULT "",
  dt INT NOT NULL DEFAULT 0,
  metainfo BLOB
);
CREATE INDEX IF NOT EXISTS torrent_history_idx1 ON torrent_history (dt DESC);
CREATE INDEX IF NOT EXISTS torrent_history_idx2 ON torrent_history (infohash);
  
`

	// Just run an a bunch of statements
	// If everything is fine - return success so we won't get in there again
	if _, err = db.Exec(sql); err == nil {
		*previousVersion = version
		success = true
	}

	return
}
