package storage

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	// CGO-free driver with SQLite embedded as WebAssembly.
	sqlitedriver "github.com/ncruces/go-sqlite3/driver"

	// FTS5 is an opt-in extension and must be registered per connection. The
	// verse search index is an FTS5 virtual table, so without this every open
	// fails with "no such module: fts5".
	"github.com/ncruces/go-sqlite3/ext/fts5"

	// Required by the driver: provides the OS filesystem VFS.
	_ "github.com/ncruces/go-sqlite3/vfs"
)

type Verse struct {
	ID          string `json:"id"`
	Translation string `json:"translation"`
	Book        string `json:"book"`
	Chapter     int    `json:"chapter"`
	Verse       int    `json:"verse"`
	Text        string `json:"text"`
}

// ResolveDBPath returns a stable database path across different working directories.
// Priority: SCHOLIA_DB_PATH -> existing cwd-relative path -> nearest ancestor path -> defaultPath.
func ResolveDBPath(defaultPath string) string {
	if override := os.Getenv("SCHOLIA_DB_PATH"); override != "" {
		if abs, err := filepath.Abs(override); err == nil {
			return abs
		}
		return override
	}

	if filepath.IsAbs(defaultPath) {
		return defaultPath
	}

	if _, err := os.Stat(defaultPath); err == nil {
		return defaultPath
	}

	cwd, err := os.Getwd()
	if err != nil {
		return defaultPath
	}

	probeDir := cwd
	for {
		candidate := filepath.Join(probeDir, defaultPath)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}

		parent := filepath.Dir(probeDir)
		if parent == probeDir {
			break
		}
		probeDir = parent
	}

	return defaultPath
}

// OpenBibleDB opens the Bible corpus read-only.
//
// This file is baked into the container image and lives on a read-only layer,
// which constrains how it may be opened:
//
//   - WAL mode is not usable. A WAL reader must create and write the -shm
//     shared-memory file even when it only ever reads, so a WAL-mode database
//     on a read-only filesystem fails to open at all. cmd/seed therefore
//     checkpoints and switches the file back to rollback journalling before
//     shipping it (see FinalizeBibleDB).
//   - The database must be self-contained. A leftover -wal sidecar would carry
//     committed data that a read-only handle cannot replay.
//
// Missing files are reported as errors rather than being created silently:
// SQLite will happily conjure an empty database, and an API serving zero verses
// is far harder to diagnose than a refusal to boot.
func OpenBibleDB(path string) (*sql.DB, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("bible database not found at %s: build it with `go run ./cmd/seed`", path)
		}
		return nil, fmt.Errorf("stat bible database %s: %w", path, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("bible database path %s is a directory", path)
	}

	if _, err := os.Stat(path + "-wal"); err == nil {
		return nil, fmt.Errorf(
			"bible database %s has an un-checkpointed -wal sidecar; it is not safe to open read-only. "+
				"Re-run `go run ./cmd/seed`, which checkpoints and finalizes the file", path)
	}

	// file: URI so query parameters are honoured. Path is escaped because
	// absolute paths on some platforms contain characters that are otherwise
	// significant in a URI.
	dsn := "file:" + url.PathEscape(path) + "?mode=ro"
	db, err := sqlitedriver.Open(dsn, fts5.Register)
	if err != nil {
		return nil, fmt.Errorf("open bible database: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("open bible database %s read-only: %w", path, err)
	}

	// Reads are concurrent and never block each other on a read-only file, so
	// the pool can be as wide as the HTTP server needs.
	db.SetMaxOpenConns(envInt("SCHOLIA_BIBLE_MAX_OPEN_CONNS", 16))
	db.SetMaxIdleConns(envInt("SCHOLIA_BIBLE_MAX_IDLE_CONNS", 8))

	return db, nil
}

// OpenBibleDBForSeed opens the corpus read-write for cmd/seed. WAL is enabled
// here purely for bulk-insert throughput; FinalizeBibleDB undoes it afterwards.
func OpenBibleDBForSeed(path string) (*sql.DB, error) {
	db, err := sqlitedriver.Open(path, fts5.Register)
	if err != nil {
		return nil, fmt.Errorf("open bible database for seeding: %w", err)
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable WAL: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	// Give SQLite a memory ceiling.
	//
	// This driver runs SQLite compiled to WebAssembly, so its allocations live
	// in the wazero module's linear memory, not the Go heap. Measured on a full
	// seed: Go heap stays around 3MB while resident memory reaches ~228MB. That
	// means GOGC and GOMEMLIMIT have no influence here whatsoever — a fact worth
	// recording, because tuning them is the obvious first instinct and it is
	// wasted effort.
	//
	// soft_heap_limit asks SQLite to reclaim rather than grow once it passes the
	// threshold. It showed no measurable effect on a machine with memory to
	// spare, which is expected — it is a relief valve for when memory is tight,
	// not an optimisation. It is set defensively and has not been proven to fix
	// any specific failure.
	if _, err := db.Exec(fmt.Sprintf("PRAGMA soft_heap_limit=%d;",
		envInt("SCHOLIA_SEED_HEAP_LIMIT_BYTES", 64<<20))); err != nil {
		db.Close()
		return nil, fmt.Errorf("set soft heap limit: %w", err)
	}

	return db, nil
}

// FinalizeBibleDB prepares a freshly seeded database for read-only shipping:
// it folds the WAL back into the main file, leaves rollback-journal mode so no
// -shm/-wal sidecars are needed at read time, and compacts the result.
//
// Call this at the end of seeding. Without it the API cannot open the file.
func FinalizeBibleDB(db *sql.DB) error {
	if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE);"); err != nil {
		return fmt.Errorf("checkpoint WAL: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=DELETE;"); err != nil {
		return fmt.Errorf("switch to rollback journal: %w", err)
	}
	// VACUUM reclaims the churn left by repeated seeding, which is substantial
	// on a corpus this size and ships in every image layer if left behind.
	if _, err := db.Exec("VACUUM;"); err != nil {
		return fmt.Errorf("vacuum: %w", err)
	}

	// Deliberately no PRAGMA optimize here. It runs ANALYZE, which on the FTS5
	// index over the full Bible text takes longer than the entire rest of the
	// seed — tens of minutes — in exchange for query-planner statistics this
	// workload does not need. The queries are simple lookups against explicit
	// indexes and FTS5 MATCH.
	//
	// If a future query does need stats, run ANALYZE against the specific table
	// rather than reinstating a blanket optimize.
	return nil
}

// CreateBibleTables provisions the corpus schema. Only cmd/seed calls this; the
// API opens the file read-only and never migrates it.
//
// User-owned data (accounts, API keys, invites, notes) deliberately does NOT
// live here. It lives in Postgres — see migrations/.
func CreateBibleTables(db *sql.DB) error {
	schema := `
    -- 1. Main Bible Text (Human Readable)
    CREATE TABLE IF NOT EXISTS verses (
        id TEXT PRIMARY KEY, -- e.g., 'BSB.MAT.1.1'
        translation TEXT,
        book TEXT,
        chapter INTEGER,
        verse INTEGER,
        text TEXT
    );

    -- 2. Full-Text Search (For lightning fast search in Next.js)
    CREATE VIRTUAL TABLE IF NOT EXISTS verses_fts USING fts5(
        osis_id UNINDEXED,
        translation UNINDEXED,
        content
    );

    -- 3. Lexicon (Original Language Dictionaries)
    CREATE TABLE IF NOT EXISTS lexicon (
        strongs_id TEXT PRIMARY KEY,
        word TEXT,
        transliteration TEXT,
        definition TEXT
    );

    -- 4. Morphology (Grammar Explanations)
    CREATE TABLE IF NOT EXISTS morphology (
        code TEXT PRIMARY KEY, -- e.g., 'V-PAI-3S'
        short_def TEXT,        -- 'Verb Present Active Indicative'
        long_exp TEXT          -- 'Detailed explanation of the function'
    );

    -- 5. Verse Analysis (The "Amalgamated" Word-by-Word link)
    -- This table connects specific words in a verse to Strongs and Morph codes
    CREATE TABLE IF NOT EXISTS verse_analysis (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        verse_id TEXT,
        word_order INTEGER,
        surface_word TEXT,     -- Original Greek/Hebrew word
        english_gloss TEXT,    -- Brief English meaning
        strongs_id TEXT,       -- Link to Lexicon
        morph_code TEXT,       -- Link to Morphology
        manuscript_type TEXT   -- N (Ancient), K (Traditional), etc.
        -- No cross-table FKs: verse_analysis is a read-model. strongs_id and
        -- morph_code are joined with LEFT JOINs at query time, and enforcing
        -- FKs here silently dropped valid tokens (or forced placeholder rows)
        -- whenever a lexicon/morphology/verse entry was missing.
    );
    CREATE INDEX IF NOT EXISTS idx_verse_analysis_verse ON verse_analysis(verse_id);
    CREATE INDEX IF NOT EXISTS idx_verse_analysis_strongs ON verse_analysis(strongs_id);

-- 6. Versification Mapping (The bridge between BSB, KJV, and Original Texts)
CREATE TABLE IF NOT EXISTS versification (
        mapping_type TEXT,   -- OneToOne, MergedPrevVerse, etc.
        kjv_ref TEXT,        -- The English/KJV standard reference
        hebrew_ref TEXT,     -- The Hebrew (MT) equivalent
        greek_ref TEXT,      -- The Greek (LXX) equivalent
        notes TEXT,          -- To store "Absent" or "NotExist" logic
        PRIMARY KEY (kjv_ref)
    );

    -- 7. Cross References
    CREATE TABLE IF NOT EXISTS cross_references (
        from_verse TEXT,
        to_verse TEXT,
        FOREIGN KEY(from_verse) REFERENCES verses(id),
        FOREIGN KEY(to_verse) REFERENCES verses(id)
    );

-- 8. Unified Geography Table
CREATE TABLE IF NOT EXISTS locations (
    id TEXT PRIMARY KEY,
    name TEXT,
    modern_name TEXT,
    latitude REAL,
    longitude REAL,
    feature_type TEXT,
    geometry_type TEXT,
    image_file TEXT,      -- Internal filename (e.g., m39ac.jpg)
    image_url TEXT,       -- Direct high-res link (e.g., Wikimedia)
    credit_url TEXT,      -- Attribution link
    image_author TEXT,    -- For the "Investigation" credits
    source_info TEXT,
    geometry TEXT         -- JSON shape data (kind, boundary ring, label line, external file). See SeedGeometry.
);

-- 9. The Verse Bridge
CREATE TABLE IF NOT EXISTS verse_locations (
    verse_id TEXT,             -- e.g., '2KG.5.12'
    location_id TEXT,
    PRIMARY KEY (verse_id, location_id),
    FOREIGN KEY(location_id) REFERENCES locations(id)
);

-- 9b. Alias map for merged place identities across sources
CREATE TABLE IF NOT EXISTS location_aliases (
    alias_id TEXT PRIMARY KEY,
    canonical_location_id TEXT NOT NULL,
    source TEXT,
    FOREIGN KEY(canonical_location_id) REFERENCES locations(id)
);

CREATE INDEX IF NOT EXISTS idx_location_aliases_canonical ON location_aliases(canonical_location_id);

-- 10. Books & Chapters (For Navigation)
CREATE TABLE IF NOT EXISTS books (
    id TEXT PRIMARY KEY,
    osis_name TEXT,
    book_name TEXT,
    testament TEXT,
    book_order INTEGER,
    slug TEXT
);

CREATE TABLE IF NOT EXISTS chapters (
    id TEXT PRIMARY KEY,
    book_id TEXT,
    osis_ref TEXT, -- e.g., 'Gen.1'
    chapter_num INTEGER,
    FOREIGN KEY(book_id) REFERENCES books(id)
);

-- 11. Historical People
CREATE TABLE IF NOT EXISTS people (
    id TEXT PRIMARY KEY,
    name TEXT,
    lookup_name TEXT,
    gender TEXT,
    birth_year INTEGER,
    death_year INTEGER,
    dictionary_text TEXT,
    slug TEXT
);

-- 12. People Groups (Tribes/Nations)
CREATE TABLE IF NOT EXISTS groups (
    id TEXT PRIMARY KEY,
    name TEXT
);

-- 13. Events (The Timeline)
CREATE TABLE IF NOT EXISTS events (
    id TEXT PRIMARY KEY,
    title TEXT,
    start_date TEXT,
    duration TEXT,
    sort_key REAL
);

-- 14. The "Relational" Verse Table
-- This is critical: Theographic uses its own IDs for verses.
-- We need this table to map 'rec7mkRL...' to 'Gen.1.1'
CREATE TABLE IF NOT EXISTS verse_id_map (
    rec_id TEXT PRIMARY KEY,
    osis_ref TEXT
);

-- 15. Multi-Way Junction Tables (The Connections)
CREATE TABLE IF NOT EXISTS event_participants (
    event_id TEXT,
    participant_id TEXT, -- Can be person_id or group_id
    PRIMARY KEY (event_id, participant_id)
);

CREATE TABLE IF NOT EXISTS group_memberships (
    group_id TEXT,
    person_id TEXT,
    FOREIGN KEY(group_id) REFERENCES groups(id),
    FOREIGN KEY(person_id) REFERENCES people(id)
);

-- 16. Bridge: Events to Verses (rec... to rec...)
CREATE TABLE IF NOT EXISTS event_verses (
    event_id TEXT,
    verse_id TEXT, -- rec... ID
    PRIMARY KEY (event_id, verse_id)
);

-- 17. Bridge: People to Verses (rec... to rec...)
CREATE TABLE IF NOT EXISTS person_verses (
    person_id TEXT,
    verse_id TEXT, -- rec... ID
    PRIMARY KEY (person_id, verse_id)
);
    `

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("create bible tables: %w", err)
	}

	return dropLegacyUserTables(db)
}

// dropLegacyUserTables removes user data left in corpus files created before
// accounts and notes moved to Postgres.
//
// A fresh corpus never has these tables — CreateBibleTables no longer defines
// them — but a database seeded by an older build still carries the rows,
// including API key and invite code hashes. Those would otherwise be baked into
// every image built from that file. The authoritative copies now live in
// Postgres, so the remnants here are dead weight and a needless disclosure.
func dropLegacyUserTables(db *sql.DB) error {
	// Order matters: note_verses references notes, api_keys/invite_codes
	// reference users.
	legacy := []string{"note_verses", "notes", "api_keys", "invite_codes", "users"}

	for _, table := range legacy {
		if _, err := db.Exec("DROP TABLE IF EXISTS " + table); err != nil {
			return fmt.Errorf("drop legacy table %s: %w", table, err)
		}
	}
	return nil
}
