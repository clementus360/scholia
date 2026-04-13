package storage

import (
	"database/sql"
	"log"
	"os"
	"path/filepath"

	// CGO-free driver and embedded SQLite for macOS/ARM64 compatibility
	_ "github.com/ncruces/go-sqlite3/driver"
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

func InitDB(filepath string) *sql.DB {
	db, err := sql.Open("sqlite3", filepath)
	if err != nil {
		log.Fatal(err)
	}

	// Performance: WAL mode is essential for concurrent reads/writes in Go
	_, err = db.Exec("PRAGMA journal_mode=WAL;")
	if err != nil {
		log.Fatalf("Failed to enable WAL: %v", err)
	}

	// Relational Integrity: Must be enabled manually in SQLite
	_, err = db.Exec("PRAGMA foreign_keys = ON;")
	if err != nil {
		log.Fatalf("Failed to enable foreign keys: %v", err)
	}

	return db
}

func CreateTables(db *sql.DB) {
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
        manuscript_type TEXT,  -- N (Ancient), K (Traditional), etc.
        FOREIGN KEY(verse_id) REFERENCES verses(id),
        FOREIGN KEY(strongs_id) REFERENCES lexicon(strongs_id),
        FOREIGN KEY(morph_code) REFERENCES morphology(code)
    );

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

    -- 8. User Data: Enhanced Notes
    CREATE TABLE IF NOT EXISTS notes (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        owner_user_id TEXT,
        title TEXT,
        main_reference TEXT,
        content TEXT,
        created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
        updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
    );

    -- 9. User Data: Links between notes and specific verses
    CREATE TABLE IF NOT EXISTS note_verses (
        note_id INTEGER,
        verse_id TEXT,
        FOREIGN KEY(note_id) REFERENCES notes(id),
        FOREIGN KEY(verse_id) REFERENCES verses(id)
    );

-- 10. Unified Geography Table
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
    source_info TEXT
);

-- 11. The Verse Bridge
CREATE TABLE IF NOT EXISTS verse_locations (
    verse_id TEXT,             -- e.g., '2KG.5.12'
    location_id TEXT,
    PRIMARY KEY (verse_id, location_id),
    FOREIGN KEY(location_id) REFERENCES locations(id)
); 

-- 11b. Alias map for merged place identities across sources
CREATE TABLE IF NOT EXISTS location_aliases (
    alias_id TEXT PRIMARY KEY,
    canonical_location_id TEXT NOT NULL,
    source TEXT,
    FOREIGN KEY(canonical_location_id) REFERENCES locations(id)
);

CREATE INDEX IF NOT EXISTS idx_location_aliases_canonical ON location_aliases(canonical_location_id);

-- 12. Books & Chapters (For Navigation)
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

-- 13. Historical People
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

-- 14. People Groups (Tribes/Nations)
CREATE TABLE IF NOT EXISTS groups (
    id TEXT PRIMARY KEY,
    name TEXT
);

-- 15. Events (The Timeline)
CREATE TABLE IF NOT EXISTS events (
    id TEXT PRIMARY KEY,
    title TEXT,
    start_date TEXT,
    duration TEXT,
    sort_key REAL
);

-- 16. The "Relational" Verse Table
-- This is critical: Theographic uses its own IDs for verses. 
-- We need this table to map 'rec7mkRL...' to 'Gen.1.1'
CREATE TABLE IF NOT EXISTS verse_id_map (
    rec_id TEXT PRIMARY KEY,
    osis_ref TEXT
);

-- 17. Multi-Way Junction Tables (The Connections)
CREATE TABLE IF NOT EXISTS event_participants (
    event_id TEXT,
    participant_id TEXT, -- Can be person_id or group_id
    FOREIGN KEY(event_id) REFERENCES events(id)
);

CREATE TABLE IF NOT EXISTS group_memberships (
    group_id TEXT,
    person_id TEXT,
    FOREIGN KEY(group_id) REFERENCES groups(id),
    FOREIGN KEY(person_id) REFERENCES people(id)
);

-- 18. Bridge: Events to Verses (rec... to rec...)
CREATE TABLE IF NOT EXISTS event_verses (
    event_id TEXT,
    verse_id TEXT, -- rec... ID
    PRIMARY KEY (event_id, verse_id)
);

-- 19. Bridge: People to Verses (rec... to rec...)
CREATE TABLE IF NOT EXISTS person_verses (
    person_id TEXT,
    verse_id TEXT, -- rec... ID
    PRIMARY KEY (person_id, verse_id)
);

-- 20. Authentication Users
CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    subject TEXT NOT NULL UNIQUE,
    display_name TEXT,
    role TEXT NOT NULL DEFAULT 'member',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS api_keys (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    label TEXT,
    scopes TEXT NOT NULL DEFAULT 'read',
    active INTEGER NOT NULL DEFAULT 1,
    last_used_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(user_id) REFERENCES users(id)
);

CREATE INDEX IF NOT EXISTS idx_api_keys_token_hash ON api_keys(token_hash);
CREATE INDEX IF NOT EXISTS idx_api_keys_user_id ON api_keys(user_id);

CREATE TABLE IF NOT EXISTS invite_codes (
    id TEXT PRIMARY KEY,
    code_hash TEXT NOT NULL UNIQUE,
    label TEXT NOT NULL,
    scopes TEXT NOT NULL DEFAULT 'read',
    created_by_user_id TEXT NOT NULL,
    consumed_by_user_id TEXT,
    consumed_api_key_id TEXT,
    consumed_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(created_by_user_id) REFERENCES users(id),
    FOREIGN KEY(consumed_by_user_id) REFERENCES users(id)
);

CREATE INDEX IF NOT EXISTS idx_invite_codes_code_hash ON invite_codes(code_hash);
CREATE INDEX IF NOT EXISTS idx_invite_codes_created_by_user_id ON invite_codes(created_by_user_id);
    `

	_, err := db.Exec(schema)
	if err != nil {
		log.Fatalf("Failed to create tables: %v", err)
	}

	ensureNotesOwnershipSchema(db)
}

func ensureNotesOwnershipSchema(db *sql.DB) {
	var columnExists bool
	rows, err := db.Query("PRAGMA table_info(notes)")
	if err != nil {
		log.Printf("Failed to inspect notes table: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			log.Printf("Failed to scan notes schema: %v", err)
			return
		}
		if name == "owner_user_id" {
			columnExists = true
		}
	}

	if !columnExists {
		if _, err := db.Exec("ALTER TABLE notes ADD COLUMN owner_user_id TEXT"); err != nil {
			log.Printf("Failed to add owner_user_id to notes: %v", err)
			return
		}
	}

	if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_notes_owner_user_id ON notes(owner_user_id)"); err != nil {
		log.Printf("Failed to create notes owner index: %v", err)
		return
	}

	var ownerCount int
	if err := db.QueryRow("SELECT COUNT(1) FROM notes WHERE owner_user_id IS NOT NULL AND owner_user_id <> ''").Scan(&ownerCount); err != nil {
		log.Printf("Failed to inspect existing note ownership: %v", err)
		return
	}
	if ownerCount > 0 {
		return
	}

	var defaultOwner string
	if err := db.QueryRow("SELECT id FROM users ORDER BY created_at ASC LIMIT 1").Scan(&defaultOwner); err != nil {
		log.Printf("No default note owner available for backfill: %v", err)
		return
	}

	if _, err := db.Exec("UPDATE notes SET owner_user_id = ? WHERE owner_user_id IS NULL OR owner_user_id = ''", defaultOwner); err != nil {
		log.Printf("Failed to backfill note ownership: %v", err)
	}
}
