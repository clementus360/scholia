package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"

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
    slug TEXT,
    division TEXT,        -- 'Pentateuch', 'Gospels', ... (Theographic bookDiv)
    year_written TEXT,    -- free text: the dataset carries ranges like '-1451'
    place_written_id TEXT -- locations.id, when the dataset names one
);

CREATE TABLE IF NOT EXISTS chapters (
    id TEXT PRIMARY KEY,
    book_id TEXT,
    osis_ref TEXT, -- e.g., 'Gen.1'
    chapter_num INTEGER,
    writer_id TEXT, -- people.id credited with this chapter (Psalms especially)
    FOREIGN KEY(book_id) REFERENCES books(id)
);

-- 10b. Bridge: books to their traditional writers.
-- Theographic stores writers as lookup slugs ("moses_2108"), not record ids,
-- so the seeder resolves them through people.lookup_name.
CREATE TABLE IF NOT EXISTS book_writers (
    book_id TEXT,
    person_id TEXT,
    PRIMARY KEY (book_id, person_id)
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
    slug TEXT,
    also_called TEXT,     -- comma-separated alternate names, as the source stores them
    birth_place_id TEXT,  -- locations.id
    death_place_id TEXT   -- locations.id
);

CREATE INDEX IF NOT EXISTS idx_people_lookup_name ON people(lookup_name);

-- 11b. Family graph. One row per direction per relation, so a lookup from
-- either person finds the tie without a UNION.
CREATE TABLE IF NOT EXISTS person_relations (
    person_id TEXT,
    related_person_id TEXT,
    relation TEXT, -- father | mother | child | sibling | partner
    PRIMARY KEY (person_id, related_person_id, relation)
);

CREATE INDEX IF NOT EXISTS idx_person_relations_person ON person_relations(person_id);

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
    sort_key REAL,
    parent_event_id TEXT,      -- events.id: the larger episode this belongs to
    predecessor_event_id TEXT, -- events.id: the event immediately before it
    notes TEXT
);

-- 13b. Bridge: events to where they happened.
CREATE TABLE IF NOT EXISTS event_locations (
    event_id TEXT,
    location_id TEXT,
    PRIMARY KEY (event_id, location_id)
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

-- 18. Reverse-lookup indexes for the verse context endpoint.
--
-- Every table below is queried BY VERSE, but each one's primary key leads with
-- the other column, so the implicit index cannot serve these lookups. Without
-- them SQLite falls back to full scans — 606,790 rows of cross_references for
-- every single verse — which made /verse/{id}/context take 206ms for one verse
-- and four seconds for a twenty-verse range. With them the same range takes
-- ~80ms.
--
-- Measured before adding any of these: the query planner reported
-- "SCAN cross_references", "SCAN pv", "SCAN ev".
CREATE INDEX IF NOT EXISTS idx_cross_references_from ON cross_references(from_verse);
CREATE INDEX IF NOT EXISTS idx_person_verses_verse   ON person_verses(verse_id);
CREATE INDEX IF NOT EXISTS idx_event_verses_verse    ON event_verses(verse_id);

-- verse_id_map translates Theographic record ids to OSIS refs. Its primary key
-- is rec_id, but the people/groups/events lookups all start from an OSIS ref.
CREATE INDEX IF NOT EXISTS idx_verse_id_map_osis     ON verse_id_map(osis_ref);

-- 18b. Per-verse chronology.
--
-- Theographic assigns a year to 28,024 of its 31,102 verses. It is the only
-- direct dating signal in the corpus: people's birth years are populated for
-- fewer than 3% of people, so anything derived from those covers almost
-- nothing. Keyed by OSIS ref because that is what callers already hold.
CREATE TABLE IF NOT EXISTS verse_years (
    osis_ref TEXT PRIMARY KEY,
    year_num INTEGER
);

-- 18c. Historical eras, joined to a verse through verse_years.
--
-- Hand-authored rather than sourced: no open dataset carries era boundaries,
-- and the ranges below follow the same traditional chronology the Theographic
-- year numbers use (its creation date is -4004, i.e. Ussher). The UI labels
-- them as traditional for that reason. Ranges are [start_year, end_year).
CREATE TABLE IF NOT EXISTS eras (
    id TEXT PRIMARY KEY,
    name TEXT,
    start_year INTEGER,
    end_year INTEGER,
    summary TEXT,
    sort_order INTEGER
);

-- 18c-2. The world outside the text: foreign rulers and world events, plus a
-- background piece per era and region.
--
-- Joined to a verse by ERA rather than by year, and deliberately so. The verse
-- years come from Theographic's traditional chronology while these dates follow
-- the conventional scholarly one, and the two disagree by up to fifty years in
-- the Old Testament — enough for a year-join to seat the wrong king beside the
-- wrong verse. Era bands are coarse enough to absorb that disagreement.
CREATE TABLE IF NOT EXISTS world_context (
    id TEXT PRIMARY KEY,
    kind TEXT, -- ruler | event
    name TEXT,
    title TEXT,
    region TEXT,
    start_year INTEGER,
    end_year INTEGER,
    era_id TEXT,
    note TEXT
);

CREATE INDEX IF NOT EXISTS idx_world_context_era ON world_context(era_id);

CREATE TABLE IF NOT EXISTS era_backgrounds (
    id TEXT PRIMARY KEY,
    era_id TEXT,
    region TEXT,
    title TEXT,
    body TEXT,
    sort_order INTEGER
);

CREATE INDEX IF NOT EXISTS idx_era_backgrounds_era ON era_backgrounds(era_id);

-- 18c-3. External historical context: encyclopedia articles about the world a
-- passage sits in, compiled by cmd/harvest and checked in as a data file.
--
-- Stored as articles plus links rather than one row per pairing, because the
-- same article is background for many passages — Nebuchadnezzar II bears on
-- most of the exile — and duplicating a lead paragraph per event would multiply
-- the same text across hundreds of rows.
--
-- extract is quoted verbatim from the source and must stay that way; revision
-- is the version it was taken from, which is what makes the quotation checkable
-- once the live page has moved on. Nothing here is model-written: see the
-- header comment on cmd/harvest for what the model was and was not used for.
CREATE TABLE IF NOT EXISTS external_articles (
    id TEXT PRIMARY KEY,
    wikidata_id TEXT,
    title TEXT,
    description TEXT, -- the one-line gloss, e.g. "King of Assyria"
    extract TEXT,     -- the article's opening, quoted
    url TEXT,
    revision INTEGER,
    retrieved TEXT,   -- ISO timestamp of the revision quoted
    license TEXT
);

-- Linked to events and books, never to a verse directly. Events already carry
-- their own verse links, so attaching there reaches exactly the verses the
-- episode covers without inventing a second verse mapping to keep in step.
CREATE TABLE IF NOT EXISTS external_article_links (
    article_id TEXT,
    scope TEXT,     -- event | book
    target_id TEXT, -- events.id or books.id
    -- history | parallel. Two different claims, kept apart deliberately: that
    -- Nebuchadnezzar II besieged Jerusalem is history of the events, while that
    -- Enuma Elis resembles Genesis 1 is a comparison between texts, and how the
    -- two relate is contested. The UI renders them in separate sections and
    -- says the app takes no view on the second.
    kind TEXT,
    relevance TEXT, -- one clause on why it bears on this passage
    rank INTEGER,
    PRIMARY KEY (scope, target_id, article_id)
);

-- The lookup runs by (scope, target_id) on every verse context request, which
-- the primary key already leads with; this index exists for the reverse case of
-- listing every passage an article backs.
CREATE INDEX IF NOT EXISTS idx_external_links_article ON external_article_links(article_id);

-- 18c. Articles reached through the corpus's own entities (cmd/resolve).
--
-- These share external_articles above rather than getting a table of their own:
-- both pipelines quote the same Wikipedia summaries, and Jerusalem's extract
-- should be stored once however it was reached.
--
-- What differs is how the link was made, and that is worth keeping. A place
-- matched on a coordinate *and* a name is the place; one matched on proximity
-- alone is merely nearby, and within 2.5km of an ancient site that is usually a
-- modern village. Relation carries that distinction to the UI, which quotes a
-- primary and reduces everything else to a chip. Method and confidence exist so
-- a bad link can be traced to the rule that made it.
CREATE TABLE IF NOT EXISTS entity_article_links (
    entity_kind TEXT, -- person | place | event | group
    entity_id   TEXT, -- people.id, locations.id, events.id or groups.id
    article_id  TEXT, -- external_articles.id
    relation    TEXT, -- primary | nearby
    confidence  INTEGER,
    method      TEXT, -- coordinate+name | coordinate | genealogy | class | name
    note        TEXT, -- reader-facing sentence on why this link exists
    PRIMARY KEY (entity_kind, entity_id, article_id)
);

-- One hop out of a resolved article, for readers who want to keep going.
-- Labels only: an extract for every neighbour would multiply the corpus for
-- clicks most readers never make, and the browser can fetch one from
-- Wikipedia's own CDN at the moment it is asked for.
CREATE TABLE IF NOT EXISTS article_neighbours (
    article_id TEXT, -- external_articles.id
    target_id  TEXT, -- Wikidata id of the neighbour
    label      TEXT,
    relation   TEXT, -- "part of", "father", "followed by"
    rank       INTEGER,
    PRIMARY KEY (article_id, target_id)
);

-- 18d. Dictionary articles (Easton's Bible Dictionary, 1897, public domain).
--
-- 6,519 articles shipped inside the Theographic export and were previously
-- never loaded. Only the fragments already copied onto people and places
-- reached the corpus, which left every article about a *thing* — Passover,
-- centurion, denarius, threshing floor — unreachable.
CREATE TABLE IF NOT EXISTS dictionary_entries (
    id TEXT PRIMARY KEY,
    term TEXT,
    term_key TEXT, -- lowercased term, so a verse's words can be matched against it
    term_id TEXT,
    body TEXT,
    match_type TEXT, -- person | place | multi | unmatched
    item_num INTEGER,
    source TEXT
);

CREATE INDEX IF NOT EXISTS idx_dictionary_entries_term ON dictionary_entries(term);
CREATE INDEX IF NOT EXISTS idx_dictionary_entries_term_key ON dictionary_entries(term_key);

-- Bridge from an article to the person or place it describes, so a verse's
-- people and places can pull their articles without matching on names.
CREATE TABLE IF NOT EXISTS dictionary_links (
    entry_id TEXT,
    target_kind TEXT, -- person | place
    target_id TEXT,
    PRIMARY KEY (entry_id, target_kind, target_id)
);

CREATE INDEX IF NOT EXISTS idx_dictionary_links_target ON dictionary_links(target_kind, target_id);

-- 19. Build metadata, so a corpus produced by an older schema can be detected
-- rather than silently served with missing indexes.
CREATE TABLE IF NOT EXISTS corpus_meta (
    key   TEXT PRIMARY KEY,
    value TEXT
);
    `

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("create bible tables: %w", err)
	}

	if _, err := db.Exec(
		`INSERT OR REPLACE INTO corpus_meta (key, value) VALUES ('schema_version', ?)`,
		strconv.Itoa(CorpusSchemaVersion),
	); err != nil {
		return fmt.Errorf("record corpus schema version: %w", err)
	}

	return dropLegacyUserTables(db)
}

// CorpusSchemaVersion identifies the shape of the generated corpus.
//
// Bump this whenever the schema changes in a way that requires a rebuild —
// notably when adding an index, since an existing corpus will not gain one on
// its own. The API compares this against the value recorded in the file and
// refuses to serve, or rebuilds, on a mismatch. Without that check a deploy
// that failed to reseed would come up looking healthy and just be slow.
const CorpusSchemaVersion = 4

// ErrCorpusOutdated reports a corpus built by an older schema version.
var ErrCorpusOutdated = errors.New("corpus was built by an older schema version")

// CheckCorpusVersion verifies the corpus at path matches CorpusSchemaVersion.
// A corpus predating the version marker reports version 0.
func CheckCorpusVersion(path string) error {
	db, err := OpenBibleDB(path)
	if err != nil {
		return err
	}
	defer db.Close()

	var recorded int
	err = db.QueryRow(`SELECT value FROM corpus_meta WHERE key = 'schema_version'`).Scan(&recorded)
	if err != nil && err != sql.ErrNoRows {
		// A corpus old enough to lack corpus_meta entirely fails here with "no
		// such table", which is itself the answer.
		recorded = 0
	}

	if recorded != CorpusSchemaVersion {
		return fmt.Errorf("%w: found version %d, expected %d — rebuild it with `go run ./cmd/seed`",
			ErrCorpusOutdated, recorded, CorpusSchemaVersion)
	}
	return nil
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
