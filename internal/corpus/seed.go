package corpus

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/clementus360/scholia/internal/storage"
)

// --- TYPES ---

type BSBJson struct {
	Translation string `json:"translation"`
	Books       []struct {
		Name     string `json:"name"`
		Chapters []struct {
			Chapter int `json:"chapter"`
			Verses  []struct {
				Verse int    `json:"verse"`
				Text  string `json:"text"`
			} `json:"verses"`
		} `json:"chapters"`
	} `json:"books"`
}

// Shared among files
type LonLat string // "36.305000,33.513542"

type AncientRecord struct {
	ID              string   `json:"id"`
	FriendlyID      string   `json:"friendly_id"`
	Types           []string `json:"types"`
	Extra           string   `json:"extra"`
	Identifications []struct {
		Media struct {
			Thumbnail struct {
				File    string `json:"file"`
				ImageID string `json:"image_id"` // <--- ADD THIS TAG
			} `json:"thumbnail"`
		} `json:"media"`
		Resolutions []struct {
			LonLat          string `json:"lonlat"`
			AncientGeometry string `json:"ancient_geometry"`
		} `json:"resolutions"`
	} `json:"identifications"`
}

type ModernRecord struct {
	ID                  string `json:"id"`
	FriendlyID          string `json:"friendly_id"`
	LonLat              string `json:"lonlat"`
	AncientAssociations map[string]struct {
		Name  string  `json:"name"`
		Score float64 `json:"score"`
	} `json:"ancient_associations"`
}

// 1. Build a smart Modern Mapping
type ModernLink struct {
	Name   string
	LonLat string
	Score  float64
}

type GeometryRecord struct {
	ID     string `json:"id"`
	Format string `json:"format"` // "point", "path", "polygon"
}

type ImageRecord struct {
	ID         string `json:"id"`
	FileURL    string `json:"file_url"` // <--- THIS IS THE WORKING WIKIMEDIA LINK
	CreditURL  string `json:"credit_url"`
	Author     string `json:"author"`
	Thumbnails map[string]struct {
		File string `json:"file"`
	} `json:"thumbnails"`
}

type ExtraFields struct {
	Osises []string `json:"osises"`
}

type TheoBase struct {
	ID     string                 `json:"id"`
	Fields map[string]interface{} `json:"fields"`
}

// Helper to extract strings from the interface map
func getString(fields map[string]interface{}, key string) string {
	if val, ok := fields[key].(string); ok {
		return val
	}
	return ""
}

func getYear(fields map[string]interface{}, key string) (int, bool) {
	if val, ok := fields[key].(float64); ok {
		return int(val), true
	}
	if val, ok := fields[key].(string); ok {
		val = strings.TrimSpace(val)
		if val == "" {
			return 0, false
		}
		parsed, err := strconv.Atoi(val)
		if err == nil {
			return parsed, true
		}
	}
	return 0, false
}

// --- UTILS ---

// bookTable is the single source of truth for book identity. Each row lists the
// canonical STEP code (used as the prefix of verses.id and everywhere else), the
// canonical display name, and every alias we may encounter across the source
// files: BSB full names (Arabic + Roman numerals), OSIS codes (cross-refs,
// geography), and STEP amalgamated codes. Everything routes through here so all
// tables share ONE verse-id vocabulary.
var bookTable = []struct {
	code, name string
	aliases    []string
}{
	{"GEN", "Genesis", []string{"gen"}},
	{"EXO", "Exodus", []string{"exo", "exod"}},
	{"LEV", "Leviticus", []string{"lev"}},
	{"NUM", "Numbers", []string{"num"}},
	{"DEU", "Deuteronomy", []string{"deu", "deut"}},
	{"JOS", "Joshua", []string{"jos", "josh"}},
	{"JDG", "Judges", []string{"jdg", "judg"}},
	{"RUT", "Ruth", []string{"rut", "ruth"}},
	{"1SA", "1 Samuel", []string{"1sa", "1sam", "i samuel"}},
	{"2SA", "2 Samuel", []string{"2sa", "2sam", "ii samuel"}},
	{"1KI", "1 Kings", []string{"1ki", "1kgs", "i kings"}},
	{"2KI", "2 Kings", []string{"2ki", "2kgs", "ii kings"}},
	{"1CH", "1 Chronicles", []string{"1ch", "1chr", "i chronicles"}},
	{"2CH", "2 Chronicles", []string{"2ch", "2chr", "ii chronicles"}},
	{"EZR", "Ezra", []string{"ezr", "ezra"}},
	{"NEH", "Nehemiah", []string{"neh"}},
	{"EST", "Esther", []string{"est", "esth"}},
	{"JOB", "Job", []string{"job"}},
	{"PSA", "Psalms", []string{"psa", "ps", "psalm"}},
	{"PRO", "Proverbs", []string{"pro", "prov"}},
	{"ECC", "Ecclesiastes", []string{"ecc", "eccl"}},
	{"SNG", "Song of Solomon", []string{"sng", "song", "song of songs", "sos"}},
	{"ISA", "Isaiah", []string{"isa"}},
	{"JER", "Jeremiah", []string{"jer"}},
	{"LAM", "Lamentations", []string{"lam"}},
	{"EZK", "Ezekiel", []string{"ezk", "ezek"}},
	{"DAN", "Daniel", []string{"dan"}},
	{"HOS", "Hosea", []string{"hos"}},
	{"JOL", "Joel", []string{"jol", "joel"}},
	{"AMO", "Amos", []string{"amo", "amos"}},
	{"OBA", "Obadiah", []string{"oba", "obad"}},
	{"JON", "Jonah", []string{"jon", "jonah"}},
	{"MIC", "Micah", []string{"mic"}},
	{"NAM", "Nahum", []string{"nam", "nah"}},
	{"HAB", "Habakkuk", []string{"hab"}},
	{"ZEP", "Zephaniah", []string{"zep", "zeph"}},
	{"HAG", "Haggai", []string{"hag"}},
	{"ZEC", "Zechariah", []string{"zec", "zech"}},
	{"MAL", "Malachi", []string{"mal"}},
	{"MAT", "Matthew", []string{"mat", "matt"}},
	{"MRK", "Mark", []string{"mrk", "mark"}},
	{"LUK", "Luke", []string{"luk", "luke"}},
	{"JHN", "John", []string{"jhn", "john"}},
	{"ACT", "Acts", []string{"act", "acts"}},
	{"ROM", "Romans", []string{"rom"}},
	{"1CO", "1 Corinthians", []string{"1co", "1cor", "i corinthians"}},
	{"2CO", "2 Corinthians", []string{"2co", "2cor", "ii corinthians"}},
	{"GAL", "Galatians", []string{"gal"}},
	{"EPH", "Ephesians", []string{"eph"}},
	{"PHP", "Philippians", []string{"php", "phil", "phi"}},
	{"COL", "Colossians", []string{"col"}},
	{"1TH", "1 Thessalonians", []string{"1th", "1thess", "i thessalonians"}},
	{"2TH", "2 Thessalonians", []string{"2th", "2thess", "ii thessalonians"}},
	{"1TI", "1 Timothy", []string{"1ti", "1tim", "i timothy"}},
	{"2TI", "2 Timothy", []string{"2ti", "2tim", "ii timothy"}},
	{"TIT", "Titus", []string{"tit", "titus"}},
	{"PHM", "Philemon", []string{"phm", "phlm", "phile"}},
	{"HEB", "Hebrews", []string{"heb"}},
	{"JAS", "James", []string{"jas", "jam", "james"}},
	{"1PE", "1 Peter", []string{"1pe", "1pet", "i peter"}},
	{"2PE", "2 Peter", []string{"2pe", "2pet", "ii peter"}},
	{"1JO", "1 John", []string{"1jo", "1jn", "1john", "i john"}},
	{"2JO", "2 John", []string{"2jo", "2jn", "2john", "ii john"}},
	{"3JO", "3 John", []string{"3jo", "3jn", "3john", "iii john"}},
	{"JUD", "Jude", []string{"jud", "jude"}},
	{"REV", "Revelation", []string{"rev", "revelation of john"}},
}

var bookAliasToCode = func() map[string]string {
	m := map[string]string{}
	for _, b := range bookTable {
		m[strings.ToLower(b.code)] = b.code
		m[strings.ToLower(b.name)] = b.code
		for _, a := range b.aliases {
			m[a] = b.code
		}
	}
	return m
}()

var bookCodeToName = func() map[string]string {
	m := map[string]string{}
	for _, b := range bookTable {
		m[b.code] = b.name
	}
	return m
}()

// canonicalBookCode maps any known spelling of a book (full name Arabic/Roman,
// OSIS code, or STEP code) to the canonical STEP code used in verses.id.
func canonicalBookCode(token string) string {
	t := strings.ToLower(strings.TrimSpace(token))
	t = strings.TrimSuffix(t, ".")
	t = strings.Join(strings.Fields(t), " ") // collapse internal whitespace
	if code, ok := bookAliasToCode[t]; ok {
		return code
	}
	up := strings.ToUpper(strings.TrimSpace(token))
	if len(up) >= 3 {
		return up[:3]
	}
	return up
}

// canonicalBookName returns the canonical display name for a STEP code.
func canonicalBookName(code string) string {
	if n, ok := bookCodeToName[code]; ok {
		return n
	}
	return code
}

func getBookCode(name string) string { return canonicalBookCode(name) }

func normalizeBookName(name string) string {
	return canonicalBookName(canonicalBookCode(name))
}

func normalizeStrongs(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if strings.Contains(id, "|") {
		id = strings.Split(id, "|")[0]
	}
	prefix := strings.ToUpper(id[0:1])
	if prefix != "G" && prefix != "H" {
		return id
	}
	re := regexp.MustCompile(`\d+`)
	digitMatch := re.FindString(id[1:])
	if digitMatch == "" {
		return id
	}
	var num int
	fmt.Sscanf(digitMatch, "%d", &num)
	return fmt.Sprintf("%s%04d", prefix, num)
}

func extractHebrewRoot(dStrongs string) string {
	re := regexp.MustCompile(`\{([^}]*)\}`)
	match := re.FindStringSubmatch(dStrongs)
	if len(match) > 1 {
		return match[1]
	}
	return dStrongs
}

func SanitizeLexicon(input string) string {
	re := regexp.MustCompile(`(?i)<[/]?level\d+>|<[/]?ref.*?>|<br\s*[/]?>`)
	input = re.ReplaceAllString(input, " ")
	reHTML := regexp.MustCompile("<[^>]*>")
	input = reHTML.ReplaceAllString(input, "")
	reCurlies := regexp.MustCompile(`\{.*?\}`)
	input = reCurlies.ReplaceAllString(input, "")
	return strings.TrimSpace(strings.Join(strings.Fields(input), " "))
}

// --- SEEDERS ---

// Build generates the Bible corpus at dbPath from the source files under
// dataDir, then finalizes it for read-only use.
//
// It writes to a temporary file and renames it into place on success. Seeding
// on top of an existing populated database is pathologically slow — duplicate
// inserts churn the FTS5 index, turning a ten-second build into tens of minutes
// — and a half-written database left behind by a crash would be served as if it
// were complete. Building fresh and renaming avoids both.
func Build(dbPath, dataDir string) error {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return fmt.Errorf("create database directory: %w", err)
	}

	tmpPath := dbPath + ".building"
	for _, stale := range []string{tmpPath, tmpPath + "-wal", tmpPath + "-shm"} {
		if err := os.Remove(stale); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("clear stale build file %s: %w", stale, err)
		}
	}

	if err := buildInto(tmpPath, dataDir); err != nil {
		os.Remove(tmpPath)
		return err
	}

	if err := os.Rename(tmpPath, dbPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("move corpus into place: %w", err)
	}

	return nil
}

func buildInto(dbPath, dataDir string) error {
	db, err := storage.OpenBibleDBForSeed(dbPath)
	if err != nil {
		return fmt.Errorf("open bible database: %w", err)
	}
	defer db.Close()

	if err := storage.CreateBibleTables(db); err != nil {
		return fmt.Errorf("create tables: %w", err)
	}

	at := func(parts ...string) string {
		return filepath.Join(append([]string{dataDir}, parts...)...)
	}

	seedBible(db, at("BSB.json"))
	seedLexiconFolder(db, at("lexicons")+string(os.PathSeparator))

	morphFiles := []string{
		at("morphology", "TEGMC - Translators Expansion of Greek Morphhology Codes - STEPBible.org CC BY.txt"),
		at("morphology", "TEHMC - Translators Expansion of Hebrew Morphology Codes - STEPBible.org CC BY.txt"),
	}
	for _, f := range morphFiles {
		seedMorphology(db, f)
	}

	seedAmalgamated(db, at("amalgamated")+string(os.PathSeparator))

	seedVersification(db, at("versification", "TVTMS - Translators Versification Traditions with Methodology for Standardisation for Eng+Heb+Lat+Grk+Others - STEPBible.org CC BY.txt"))

	SeedGeographySuite(db, at("geography"))

	SeedTheographicData(db, at("history"))

	// After all locations (ancient + theographic) exist and are merged, attach
	// map-shape geometry from geometry.jsonl by matching on location name.
	SeedGeometry(db, at("geography"))

	SeedCrossReferences(db, at("crossreference", "cross_references.txt"))

	// Fold the WAL back in and leave a single self-contained file. The API
	// opens this database read-only, possibly from an immutable image layer,
	// where WAL mode cannot work: a WAL reader has to write the -shm sidecar
	// even when it only reads. Skipping this step produces a corpus the API
	// refuses to open.
	log.Print("Finalizing database for read-only use...")
	if err := storage.FinalizeBibleDB(db); err != nil {
		return fmt.Errorf("finalize database: %w", err)
	}

	// Verify before publishing. A corpus that is missing its text is far more
	// dangerous than a build that failed loudly, because the API would start
	// cleanly and serve an empty Bible.
	var verseCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM verses").Scan(&verseCount); err != nil {
		return fmt.Errorf("verify corpus: %w", err)
	}
	if verseCount == 0 {
		return fmt.Errorf("corpus built with zero verses: check that source data exists under %s", dataDir)
	}
	log.Printf("Corpus built: %d verses", verseCount)

	return nil
}

func seedBible(db *sql.DB, path string) {
	file, err := os.ReadFile(path)
	if err != nil {
		log.Printf("⚠️ Could not read Bible JSON: %v", err)
		return
	}

	var data BSBJson
	json.Unmarshal(file, &data)

	tx, _ := db.Begin()
	stmt, _ := tx.Prepare(`INSERT OR REPLACE INTO verses (id, translation, book, chapter, verse, text) VALUES (?, ?, ?, ?, ?, ?)`)
	ftsStmt, _ := tx.Prepare(`INSERT INTO verses_fts (osis_id, translation, content) VALUES (?, ?, ?)`)

	for _, book := range data.Books {
		bookCode := canonicalBookCode(book.Name)
		bookName := canonicalBookName(bookCode)
		for _, chap := range book.Chapters {
			for _, v := range chap.Verses {
				osisID := fmt.Sprintf("%s.%d.%d", bookCode, chap.Chapter, v.Verse)
				stmt.Exec(osisID, "BSB", bookName, chap.Chapter, v.Verse, v.Text)
				ftsStmt.Exec(osisID, "BSB", v.Text)
			}
		}
	}
	tx.Commit()
	fmt.Println("✅ Bible text seeded.")
}

func seedLexiconFolder(db *sql.DB, folderPath string) {
	files, _ := filepath.Glob(filepath.Join(folderPath, "*.txt"))
	for _, path := range files {
		seedLexicon(db, path)
	}
}

func seedLexicon(db *sql.DB, path string) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	tx, _ := db.Begin()
	// INSERT OR IGNORE (not REPLACE): a single eStrong number (e.g. G0001) is
	// shared by several disambiguated senses (G0001G=Alpha the letter,
	// G0001H=ah! the interjection). REPLACE let the LAST sense win — and let the
	// later-globbed TFLSJ file clobber every TBESG entry. IGNORE keeps the first
	// (base) sense and only fills genuinely-missing keys from later files.
	stmt, _ := tx.Prepare(`INSERT OR IGNORE INTO lexicon (strongs_id, word, transliteration, definition) VALUES (?, ?, ?, ?)`)
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	// A real data row's first tab-field is a Strong's code like "G0001"/"H0001".
	// Keying on that (instead of a doc line that merely starts with "eStrong")
	// skips the intro prose AND the appended names dictionary, whose rows used to
	// leak in as junk ("- Named", "$========== PERSON(s)").
	isStrong := regexp.MustCompile(`^[GH][0-9]`)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, "\t")
		if len(parts) < 8 || !isStrong.MatchString(strings.TrimSpace(parts[0])) {
			continue
		}
		sID := normalizeStrongs(parts[0])
		stmt.Exec(sID, parts[3], parts[4], SanitizeLexicon(parts[7]))
	}
	tx.Commit()
	fmt.Printf("✅ Lexicon seeded: %s\n", filepath.Base(path))
}

func seedMorphology(db *sql.DB, path string) {
	content, err := os.ReadFile(path)
	if err != nil {
		return
	}
	entries := strings.Split(string(content), "$")
	tx, _ := db.Begin()
	stmt, _ := tx.Prepare(`INSERT OR REPLACE INTO morphology (code, short_def, long_exp) VALUES (?, ?, ?)`)
	for _, entry := range entries {
		lines := strings.Split(strings.TrimSpace(entry), "\n")
		if len(lines) >= 3 {
			code := strings.Fields(lines[0])[0]
			stmt.Exec(code, lines[1], lines[2])
		}
	}
	tx.Commit()
	fmt.Printf("✅ Morphology seeded: %s\n", filepath.Base(path))
}

func seedAmalgamated(db *sql.DB, folderPath string) {
	files, _ := filepath.Glob(filepath.Join(folderPath, "*.txt"))

	// Load the set of real BSB verse ids (seeded before this step) so we can
	// attach analysis only to verses that actually exist in the text.
	realVerseIDs := map[string]struct{}{}
	if rows, err := db.Query("SELECT id FROM verses"); err == nil {
		for rows.Next() {
			var id string
			if rows.Scan(&id) == nil {
				realVerseIDs[id] = struct{}{}
			}
		}
		rows.Close()
	}

	tx, _ := db.Begin()
	stmt, _ := tx.Prepare(`
        INSERT OR REPLACE INTO verse_analysis
        (verse_id, word_order, surface_word, english_gloss, strongs_id, morph_code, manuscript_type)
        VALUES (?, ?, ?, ?, ?, ?, ?)`)

	totalWords := 0
	droppedNoVerse := 0
	for _, path := range files {
		fileName := filepath.Base(path)
		isHebrew := strings.Contains(fileName, "TAHOT")
		file, _ := os.Open(path)
		scanner := bufio.NewScanner(file)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)

		inData := false
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "Ref & Type") || strings.Contains(line, "Word & Type") {
				inData = true
				continue
			}
			if !inData || strings.HasPrefix(line, "=") || strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
				continue
			}

			parts := strings.Split(line, "\t")
			minCols := 4 // Greek needs parts[0..3]
			if isHebrew {
				minCols = 6 // Hebrew reads parts[5]
			}
			if len(parts) < minCols {
				continue
			}

			// --- Clean Reference ---
			// normalizeRef canonicalises the book code (1Jn -> 1JO, matching the
			// BSB verses table) and strips the alternate-versification
			// parenthetical: "Psa.3.1(3.2)" -> "PSA.3.1" (the outer number is the
			// English/BSB numbering; the parenthetical is Hebrew). Superscription
			// tokens like "Psa.3.0" have no BSB verse and are dropped below.
			rawRef := parts[0]
			refSplit := strings.Split(rawRef, "#")
			verseID := normalizeRef(refSplit[0])

			wordOrder := "0"
			mType := "L"
			if len(refSplit) > 1 {
				metaSplit := strings.Split(refSplit[1], "=")
				wordOrder = metaSplit[0]
				if len(metaSplit) > 1 {
					mType = metaSplit[1]
				}
			}

			// --- Field Mapping ---
			var strongsID, morphCode, surfaceWord, gloss string
			if isHebrew {
				surfaceWord = strings.TrimSpace(parts[1])
				gloss = strings.TrimSpace(parts[3])
				strongsID = normalizeStrongs(extractHebrewRoot(parts[4]))
				morphCode = strings.TrimSpace(parts[5])
			} else {
				surfaceWord = strings.TrimSpace(parts[1])
				gloss = strings.TrimSpace(parts[2])
				sm := strings.Split(parts[3], "=")
				if len(sm) > 0 {
					strongsID = normalizeStrongs(sm[0])
				}
				if len(sm) > 1 {
					morphCode = sm[1]
				}
			}

			// verse_analysis no longer carries cross-table FKs, so inserts never
			// fail on a missing verse/lexicon/morphology row and we no longer
			// fabricate "[Text pending...]" verses. We only skip tokens whose
			// verse has no home in the BSB text (e.g. Hebrew Psalm
			// superscriptions numbered ".0", which BSB does not render).
			if _, ok := realVerseIDs[verseID]; !ok {
				droppedNoVerse++
				continue
			}
			if _, err := stmt.Exec(verseID, wordOrder, surfaceWord, gloss, strongsID, morphCode, mType); err == nil {
				totalWords++
			}
		}
		file.Close()
	}
	tx.Commit()
	fmt.Printf("✅ Analysis seeded: %d tokens (%d skipped: no matching BSB verse).\n", totalWords, droppedNoVerse)
}

func seedVersification(db *sql.DB, path string) {
	file, err := os.Open(path)
	if err != nil {
		log.Printf("⚠️ Could not open versification file: %v", err)
		return
	}
	defer file.Close()

	tx, _ := db.Begin()
	// Ensure table is clean before seeding to avoid the Section 6/7 conflict
	tx.Exec("DROP TABLE IF EXISTS versification;")
	tx.Exec(`CREATE TABLE versification (
		mapping_type TEXT,
		kjv_ref TEXT,
		hebrew_ref TEXT,
		greek_ref TEXT,
		notes TEXT,
		PRIMARY KEY (kjv_ref)
	);`)

	stmt, _ := tx.Prepare(`INSERT OR REPLACE INTO versification 
		(mapping_type, kjv_ref, hebrew_ref, greek_ref, notes) VALUES (?, ?, ?, ?, ?)`)

	scanner := bufio.NewScanner(file)
	// Dynamic column tracking
	kjvCol, hebCol, lxxCol := -1, -1, -1

	for scanner.Scan() {
		line := scanner.Text()

		// 1. Skip logic: Ignore definitions, empty lines, and the "TEST" check rows
		if strings.TrimSpace(line) == "" ||
			strings.HasPrefix(line, "TEST:") ||
			strings.Contains(line, "occurs when") || // Skips the explanation block
			strings.HasPrefix(line, "REFs") {
			continue
		}

		// 2. Header Detection: $Gen...
		if strings.HasPrefix(line, "$") {
			parts := strings.Split(line, "\t")
			// Reset columns for the new section
			kjvCol, hebCol, lxxCol = -1, -1, -1
			for i, p := range parts {
				p = strings.ToLower(p)
				if strings.Contains(p, "kjv") {
					kjvCol = i
				}
				if strings.Contains(p, "hebrew") {
					hebCol = i
				}
				// Matches "Greek" and "Greek*"
				if strings.Contains(p, "greek") {
					lxxCol = i
				}
			}
			continue
		}

		// 3. Data Processing
		parts := strings.Split(line, "\t")
		if kjvCol == -1 || len(parts) <= kjvCol {
			continue
		}

		mappingType := parts[0]
		kjvRaw := parts[kjvCol]

		// Skip if the KJV cell doesn't look like a reference (e.g., just "Absent")
		if !strings.Contains(kjvRaw, ".") && !strings.Contains(kjvRaw, ":") {
			continue
		}

		hebRaw := ""
		if hebCol != -1 && hebCol < len(parts) {
			hebRaw = parts[hebCol]
		}

		lxxRaw := ""
		if lxxCol != -1 && lxxCol < len(parts) {
			lxxRaw = parts[lxxCol]
		}

		// Use the expandAndInsert helper to handle ranges like Gen.32:1-32
		expandAndInsert(stmt, mappingType, kjvRaw, hebRaw, lxxRaw)
	}

	tx.Commit()
	fmt.Println("✅ Versification Logic Unified across OT and NT.")
}

func expandAndInsert(stmt *sql.Stmt, mType, kjvRaw, hebRaw, lxxRaw string) {
	// 1. Identify if this is a range (e.g., "Exo.37:1-3")
	// If it contains a '-', we need to find the start and end verse
	re := regexp.MustCompile(`(\d+)-(\d+)`)
	matches := re.FindStringSubmatch(kjvRaw)

	if len(matches) == 3 {
		start, _ := strconv.Atoi(matches[1])
		end, _ := strconv.Atoi(matches[2])

		// Get the book/chapter prefix (e.g., "Exo.37:")
		prefix := kjvRaw[:strings.Index(kjvRaw, ":")+1]

		for i := start; i <= end; i++ {
			kjvVerse := normalizeRef(fmt.Sprintf("%s%d", prefix, i))

			// For simplicity, we map the range to the same target refs
			// Advanced mapping would require shifting the target too
			heb := normalizeRef(hebRaw)
			lxx := normalizeRef(lxxRaw)

			notes := ""
			if strings.Contains(strings.ToUpper(lxx), "ABSENT") {
				notes = lxx
				lxx = ""
			}

			stmt.Exec(mType, kjvVerse, heb, lxx, notes)
		}
	} else {
		// Just a single verse
		kjv := normalizeRef(kjvRaw)
		heb := normalizeRef(hebRaw)
		lxx := normalizeRef(lxxRaw)

		notes := ""
		if strings.Contains(strings.ToUpper(lxx), "ABSENT") {
			notes = lxx
			lxx = ""
		}
		stmt.Exec(mType, kjv, heb, lxx, notes)
	}
}

// normalizeRef turns any verse reference ("2Kgs.5:12", "Exo.37:1-3",
// "Psa.3.1(3.2)", "1Jn.1.1") into the canonical verses.id form ("2KI.5.12",
// "EXO.37.1", "PSA.3.1", "1JO.1.1"). The book token is routed through
// canonicalBookCode so OSIS/STEP/full-name spellings all converge; alternate
// versification parentheticals and range ends are dropped.
func normalizeRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if i := strings.IndexByte(ref, '('); i >= 0 { // strip "(3.2)" alt-versification
		ref = ref[:i]
	}
	if i := strings.IndexByte(ref, '-'); i >= 0 { // keep the start of a range
		ref = ref[:i]
	}
	ref = strings.ReplaceAll(ref, ":", ".")
	ref = strings.ReplaceAll(ref, " ", ".")
	ref = strings.TrimSpace(ref)
	parts := strings.SplitN(ref, ".", 2)
	if len(parts) < 2 {
		return canonicalBookCode(ref)
	}
	return canonicalBookCode(parts[0]) + "." + strings.ToUpper(parts[1])
}

// expandRefRange turns a possibly-ranged reference ("Col.1.16-Col.1.17") into
// the list of individual canonical verse ids it covers. Same-chapter numeric
// ranges are fully expanded; cross-chapter ranges keep both endpoints.
func expandRefRange(ref string) []string {
	ref = strings.TrimSpace(ref)
	start := normalizeRef(ref)
	if !strings.Contains(ref, "-") {
		return []string{start}
	}
	seg := strings.SplitN(ref, "-", 2)
	endNorm := normalizeRef(seg[1])
	sp := strings.Split(start, ".")
	ep := strings.Split(endNorm, ".")
	if len(sp) != 3 {
		return []string{start}
	}
	// The end may be a full ref ("Col.1.17") or a bare verse number ("17").
	endBook, endChap, endVerse := sp[0], sp[1], ""
	switch len(ep) {
	case 3:
		endBook, endChap, endVerse = ep[0], ep[1], ep[2]
	case 1:
		endVerse = ep[0]
	default:
		return []string{start}
	}
	if endBook != sp[0] || endChap != sp[1] {
		return []string{start, endNorm} // cross-chapter: don't invent verses
	}
	s, err1 := strconv.Atoi(sp[2])
	e, err2 := strconv.Atoi(endVerse)
	if err1 != nil || err2 != nil || e < s || e-s > 300 {
		return []string{start}
	}
	out := make([]string, 0, e-s+1)
	for i := s; i <= e; i++ {
		out = append(out, fmt.Sprintf("%s.%s.%d", sp[0], sp[1], i))
	}
	return out
}

func SeedGeographySuite(db *sql.DB, baseDir string) {
	// 1. Pre-load Modern coords into memory
	modernLookup := make(map[string]ModernLink)

	loadJSONL(baseDir+"/modern.jsonl", func(line []byte) {
		var m ModernRecord
		if err := json.Unmarshal(line, &m); err == nil {
			for ancientID, assoc := range m.AncientAssociations {
				// Only update if this modern location has a better score than what we've found so far
				if existing, ok := modernLookup[ancientID]; !ok || assoc.Score > existing.Score {
					modernLookup[ancientID] = ModernLink{
						Name:   m.FriendlyID,
						LonLat: m.LonLat,
						Score:  assoc.Score,
					}
				}
			}
		}
	})

	// 2. Pre-load Geometry types
	geomTypes := make(map[string]string)
	loadJSONL(baseDir+"/geometry.jsonl", func(line []byte) {
		var r GeometryRecord
		if err := json.Unmarshal(line, &r); err == nil {
			geomTypes[r.ID] = r.Format
		}
	})

	// 3. Pre-load Image metadata (keyed by Image ID like 'i000acb')
	imageDetails := make(map[string]ImageRecord)
	loadJSONL(baseDir+"/image.jsonl", func(line []byte) {
		var r ImageRecord
		if err := json.Unmarshal(line, &r); err == nil {
			imageDetails[r.ID] = r
		}
	})

	// 4. Final Pass: Process Ancient.jsonl
	tx, err := db.Begin()
	if err != nil {
		log.Fatalf("Failed to begin transaction: %v", err)
	}
	defer tx.Rollback()

	locStmt, err := tx.Prepare(`INSERT OR REPLACE INTO locations 
        (id, name, modern_name, latitude, longitude, feature_type, geometry_type, image_file, image_url, credit_url, image_author) 
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		log.Fatalf("Failed to prepare locStmt: %v", err)
	}
	defer locStmt.Close()

	bridgeStmt, err := tx.Prepare(`INSERT OR IGNORE INTO verse_locations (verse_id, location_id) VALUES (?, ?)`)
	if err != nil {
		log.Fatalf("Failed to prepare bridge statement: %v", err)
	}
	defer bridgeStmt.Close()

	loadJSONL(baseDir+"/ancient.jsonl", func(line []byte) {
		var a AncientRecord
		if err := json.Unmarshal(line, &a); err != nil {
			return
		}

		// A. COORDINATES & MODERN NAME MAPPING
		var lat, lon float64
		var geomType string
		var modernName string

		// Check our smart Modern Lookup map first for the "Identification"
		if modern, ok := modernLookup[a.ID]; ok {
			modernName = modern.Name
			lat, lon = parseLonLat(modern.LonLat)
		}

		// B. RESOLUTIONS (Override coordinates if specific ancient geometry exists)
		if len(a.Identifications) > 0 && len(a.Identifications[0].Resolutions) > 0 {
			res := a.Identifications[0].Resolutions[0]

			// Only override coordinates if they are valid (not 0,0)
			l, ln := parseLonLat(res.LonLat)
			if l != 0 || ln != 0 {
				lat, lon = l, ln
			}
			geomType = res.AncientGeometry
		}

		// Fallback for Geometry Type from geometry.jsonl map
		if geomType == "" {
			geomType = geomTypes[a.ID]
		}

		// C. IMAGE LOGIC (The Fix)
		var imgFile, imgURL, imgCredit, imgAuthor string

		// Get the Image ID from the ancient record's media block
		var targetImageID string
		if len(a.Identifications) > 0 && a.Identifications[0].Media.Thumbnail.ImageID != "" {
			targetImageID = a.Identifications[0].Media.Thumbnail.ImageID
			imgFile = a.Identifications[0].Media.Thumbnail.File
		}

		// Look up that Image ID in our imageDetails map (built from image.jsonl)
		if details, ok := imageDetails[targetImageID]; ok {
			imgURL = details.FileURL      // Direct Wikimedia link
			imgCredit = details.CreditURL // Source link
			imgAuthor = details.Author
		}

		// D. FALLBACK IMAGE URL
		// If there's no Wikimedia URL, but we have a filename, use the OpenBible CDN
		if imgURL == "" && imgFile != "" {
			imgURL = "https://www.openbible.info/geo/img/" + imgFile
		}

		// E. FEATURE TYPE
		fType := ""
		if len(a.Types) > 0 {
			fType = a.Types[0]
		}

		// EXECUTE INSERT
		_, err = locStmt.Exec(
			a.ID,
			a.FriendlyID, // The Ancient Name (e.g., Bethphage)
			modernName,   // The Modern Association (e.g., Abu Dis)
			lat,
			lon,
			fType,
			geomType,
			imgFile,
			imgURL,
			imgCredit,
			imgAuthor,
		)
		if err != nil {
			log.Printf("Insert error for %s: %v", a.FriendlyID, err)
		}

		// F. VERSE LINKING (The Bridge)
		var extra struct {
			Osises []string `json:"osises"`
		}
		// Parsing the nested JSON inside the "extra" string
		json.Unmarshal([]byte(a.Extra), &extra)
		for _, osis := range extra.Osises {
			_, err := bridgeStmt.Exec(normalizeRef(osis), a.ID)
			if err != nil {
				log.Printf("Bridge error for %s -> %s: %v", osis, a.ID, err)
			}
		}
	})

	if err := tx.Commit(); err != nil {
		log.Fatalf("Failed to commit: %v", err)
	}
	log.Println("✅ Geography integrated with working Wikimedia links.")
}

// --- Geometry preservation ---
//
// geometry.jsonl describes the map SHAPE of each geographic feature: whether it
// is land or water, what kind of shape it is (polygon region, path/route,
// isobands = probability heat-bands for uncertain sites, etc.), and — for a
// subset of records — an inline coordinate ring under "suggested". The full,
// detailed shapes live in external .geojson files referenced by *_geojson_file
// (not shipped in this repo). We preserve BOTH: the inline coordinates we have,
// and the external filename so the client can fetch full detail once hosted.

type geometrySuggested struct {
	RoughBoundary       []string `json:"rough_boundary"`
	LabelLine           []string `json:"label_line"`
	LabelLineHorizontal []string `json:"label_line_horizontal"`
}

type geometryRecord struct {
	Name              string             `json:"name"`
	Geometry          string             `json:"geometry"`
	LandOrWater       string             `json:"land_or_water"`
	GeojsonFile       string             `json:"geojson_file"`
	SimplifiedGeojson string             `json:"simplified_geojson_file"`
	IsobandsGeojson   string             `json:"isobands_geojson_file"`
	Suggested         *geometrySuggested `json:"suggested"`
}

// geometryOut is the JSON shape stored on locations.geometry and sent to the
// client. Coordinates are [lon, lat] pairs (GeoJSON axis order).
type geometryOut struct {
	Kind         string       `json:"kind"`
	LandOrWater  string       `json:"land_or_water,omitempty"`
	Boundary     [][2]float64 `json:"boundary,omitempty"`
	LabelLine    [][2]float64 `json:"label_line,omitempty"`
	ExternalFile string       `json:"external_file,omitempty"`
}

func parseCoordList(pairs []string) [][2]float64 {
	out := make([][2]float64, 0, len(pairs))
	for _, p := range pairs {
		xy := strings.Split(p, ",")
		if len(xy) != 2 {
			continue
		}
		lon, err1 := strconv.ParseFloat(strings.TrimSpace(xy[0]), 64)
		lat, err2 := strconv.ParseFloat(strings.TrimSpace(xy[1]), 64)
		if err1 != nil || err2 != nil {
			continue
		}
		out = append(out, [2]float64{lon, lat})
	}
	return out
}

func SeedGeometry(db *sql.DB, baseDir string) {
	tx, err := db.Begin()
	if err != nil {
		log.Printf("⚠️ geometry: begin tx: %v", err)
		return
	}
	// Index existing locations by normalized name (built after all merges).
	_, byName, err := loadLocationIndex(tx)
	if err != nil {
		log.Printf("⚠️ geometry: load index: %v", err)
		tx.Rollback()
		return
	}

	// Fill when empty; a record WITH inline coordinates (hasCoords=1) always wins
	// over a coordinate-less one so multiple records per location don't clobber
	// the usable geometry.
	stmt, err := tx.Prepare(`UPDATE locations SET geometry = ?
		WHERE id = ? AND (geometry IS NULL OR geometry = '' OR ? = 1)`)
	if err != nil {
		log.Printf("⚠️ geometry: prepare: %v", err)
		tx.Rollback()
		return
	}
	defer stmt.Close()

	attached, withCoords, unmatched := 0, 0, 0
	loadJSONL(baseDir+"/geometry.jsonl", func(line []byte) {
		var r geometryRecord
		if json.Unmarshal(line, &r) != nil {
			return
		}
		id, ok := byName[normalizeLocationName(r.Name)]
		if !ok {
			unmatched++ // mostly modern/non-biblical features not in our table
			return
		}

		out := geometryOut{Kind: r.Geometry, LandOrWater: r.LandOrWater}
		switch { // prefer the smallest full-detail file the client would fetch
		case r.SimplifiedGeojson != "":
			out.ExternalFile = r.SimplifiedGeojson
		case r.GeojsonFile != "":
			out.ExternalFile = r.GeojsonFile
		case r.IsobandsGeojson != "":
			out.ExternalFile = r.IsobandsGeojson
		}
		if r.Suggested != nil {
			out.Boundary = parseCoordList(r.Suggested.RoughBoundary)
			out.LabelLine = parseCoordList(r.Suggested.LabelLine)
			if len(out.LabelLine) == 0 {
				out.LabelLine = parseCoordList(r.Suggested.LabelLineHorizontal)
			}
		}

		blob, err := json.Marshal(out)
		if err != nil {
			return
		}
		hasCoords := 0
		if len(out.Boundary) > 0 {
			hasCoords = 1
		}
		if res, err := stmt.Exec(string(blob), id, hasCoords); err == nil {
			if n, _ := res.RowsAffected(); n > 0 {
				attached++
				withCoords += hasCoords
			}
		}
	})

	if err := tx.Commit(); err != nil {
		log.Printf("⚠️ geometry: commit: %v", err)
		return
	}
	fmt.Printf("✅ Geometry attached to %d locations (%d with inline coordinates; %d source features unmatched).\n",
		attached, withCoords, unmatched)
}

// loadJSONL opens a file and executes a callback function for every line.
// This allows us to process different files (Ancient, Modern, etc.)
// using the same streaming logic.
func loadJSONL(path string, processor func([]byte)) {
	file, err := os.Open(path)
	if err != nil {
		log.Printf("⚠️ File missing: %s. Skipping...", path)
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	// Use a large buffer (1MB) to handle potentially long JSON lines
	// common in geography/geometry files
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		processor(line)
	}

	if err := scanner.Err(); err != nil {
		log.Printf("❌ Error reading %s: %v", path, err)
	}
}

// Helper to parse "Lon,Lat" string safely
func parseLonLat(s string) (float64, float64) {
	parts := strings.Split(s, ",")
	if len(parts) != 2 {
		return 0, 0
	}
	lon, _ := strconv.ParseFloat(parts[0], 64)
	lat, _ := strconv.ParseFloat(parts[1], 64)
	return lat, lon
}

func normalizeLocationName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return ""
	}
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.ReplaceAll(name, "_", " ")
	return strings.Join(strings.Fields(name), " ")
}

func locationMergeKey(name, featureType string) string {
	return normalizeLocationName(name) + "|" + strings.ToLower(strings.TrimSpace(featureType))
}

func loadLocationIndex(tx *sql.Tx) (map[string]string, map[string]string, error) {
	rows, err := tx.Query("SELECT id, name, COALESCE(feature_type, '') FROM locations")
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	byNameAndType := make(map[string]string)
	byName := make(map[string]string)
	for rows.Next() {
		var id, name, featureType string
		if err := rows.Scan(&id, &name, &featureType); err != nil {
			return nil, nil, err
		}
		nameKey := normalizeLocationName(name)
		if nameKey == "" {
			continue
		}
		if _, exists := byName[nameKey]; !exists {
			byName[nameKey] = id
		}
		if _, exists := byNameAndType[locationMergeKey(name, featureType)]; !exists {
			byNameAndType[locationMergeKey(name, featureType)] = id
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return byNameAndType, byName, nil
}

func SeedTheographicData(db *sql.DB, baseDir string) {
	tx, _ := db.Begin()
	nameTypeToCanonical, nameToCanonical, err := loadLocationIndex(tx)
	if err != nil {
		log.Printf("⚠️ Could not load location normalization index: %v", err)
		nameTypeToCanonical = map[string]string{}
		nameToCanonical = map[string]string{}
	}

	// 1. Map Verse IDs (THE TRANSLATOR)
	seedFile(baseDir+"/verses.json", func(id string, f map[string]interface{}) {
		tx.Exec("INSERT OR REPLACE INTO verse_id_map (rec_id, osis_ref) VALUES (?, ?)",
			id, getString(f, "osisRef"))
	})

	// 2. Seed Books (Fixed: bookOrder is usually a number)
	seedFile(baseDir+"/books.json", func(id string, f map[string]interface{}) {
		order := 0
		if val, ok := f["bookOrder"].(float64); ok {
			order = int(val)
		}

		tx.Exec(`INSERT OR REPLACE INTO books (id, osis_name, book_name, testament, book_order, slug) 
				VALUES (?, ?, ?, ?, ?, ?)`,
			id, getString(f, "osisName"), getString(f, "bookName"),
			getString(f, "testament"), order, getString(f, "slug"))
	})

	// 3. Seed Chapters (Fixed: Extracting book ID from Array)
	seedFile(baseDir+"/chapters.json", func(id string, f map[string]interface{}) {
		bookID := ""
		if ids, ok := f["book"].([]interface{}); ok && len(ids) > 0 {
			bookID = ids[0].(string)
		}
		chNum := 0
		if val, ok := f["chapterNum"].(float64); ok {
			chNum = int(val)
		}

		tx.Exec("INSERT OR REPLACE INTO chapters (id, book_id, osis_ref, chapter_num) VALUES (?, ?, ?, ?)",
			id, bookID, getString(f, "osisRef"), chNum)
	})

	// 4. Seed Places (Fixed: Using kjvName and featureType)
	seedFile(baseDir+"/places.json", func(id string, f map[string]interface{}) {
		name := getString(f, "kjvName")
		featureType := getString(f, "featureType")
		desc := ""
		if d, ok := f["dictText"].([]interface{}); ok && len(d) > 0 {
			desc = d[0].(string)
		}

		targetID := id
		if canonical, ok := nameTypeToCanonical[locationMergeKey(name, featureType)]; ok {
			targetID = canonical
		} else if canonical, ok := nameToCanonical[normalizeLocationName(name)]; ok {
			targetID = canonical
		}

		if targetID == id {
			tx.Exec(`INSERT OR REPLACE INTO locations (id, name, feature_type, source_info) 
				VALUES (?, ?, ?, ?)`,
				id, name, featureType, desc)

			nameKey := normalizeLocationName(name)
			if nameKey != "" {
				if _, exists := nameToCanonical[nameKey]; !exists {
					nameToCanonical[nameKey] = id
				}
				if _, exists := nameTypeToCanonical[locationMergeKey(name, featureType)]; !exists {
					nameTypeToCanonical[locationMergeKey(name, featureType)] = id
				}
			}
		} else {
			tx.Exec(`INSERT OR REPLACE INTO location_aliases (alias_id, canonical_location_id, source)
				VALUES (?, ?, ?)`, id, targetID, "theographic")

			tx.Exec(`UPDATE locations
				SET feature_type = CASE WHEN COALESCE(feature_type, '') = '' THEN ? ELSE feature_type END,
					source_info = CASE
						WHEN ? = '' THEN source_info
						WHEN COALESCE(source_info, '') = '' THEN ?
						WHEN instr(source_info, ?) > 0 THEN source_info
						ELSE source_info || '\n\n' || ?
					END
				WHERE id = ?`, featureType, desc, desc, desc, desc, targetID)
		}

		// Link Place to Verses
		if verses, ok := f["verses"].([]interface{}); ok {
			for _, vID := range verses {
				tx.Exec("INSERT OR IGNORE INTO verse_locations (location_id, verse_id) VALUES (?, ?)", targetID, vID)
			}
		}
	})

	// 5. Seed People (Fixed: Adding person_verses bridge)
	seedFile(baseDir+"/people.json", func(id string, f map[string]interface{}) {
		// FIX 1: Handle dictText Array
		dictionaryText := ""
		if d, ok := f["dictText"].([]interface{}); ok && len(d) > 0 {
			dictionaryText = d[0].(string)
		}

		// Prefer explicit lifespan fields; use min/max as fallback only.
		bYear, ok := getYear(f, "birthYear")
		if !ok {
			bYear, _ = getYear(f, "minYear")
		}

		dYear, ok := getYear(f, "deathYear")
		if !ok {
			dYear, _ = getYear(f, "maxYear")
		}

		// EXECUTE INSERT
		tx.Exec(`INSERT OR REPLACE INTO people 
            (id, name, lookup_name, gender, birth_year, death_year, dictionary_text, slug) 
            VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			id,
			getString(f, "name"),
			getString(f, "personLookup"),
			getString(f, "gender"),
			bYear,
			dYear,
			dictionaryText,
			getString(f, "slug"),
		)

		// FIX 3: Link People to Verses (Crucial for lookups)
		if verses, ok := f["verses"].([]interface{}); ok {
			for _, vID := range verses {
				tx.Exec("INSERT OR IGNORE INTO person_verses (person_id, verse_id) VALUES (?, ?)", id, vID.(string))
			}
		}
	})

	// 6. Seed Events (event_verses + event_participants bridges)
	seedFile(baseDir+"/events.json", func(id string, f map[string]interface{}) {
		tx.Exec("INSERT OR REPLACE INTO events (id, title, start_date, duration, sort_key) VALUES (?, ?, ?, ?, ?)",
			id, getString(f, "title"), getString(f, "startDate"), getString(f, "duration"), f["sortKey"])

		if verses, ok := f["verses"].([]interface{}); ok {
			for _, vID := range verses {
				if s, ok := vID.(string); ok {
					tx.Exec("INSERT OR IGNORE INTO event_verses (event_id, verse_id) VALUES (?, ?)", id, s)
				}
			}
		}

		// participants links people (and occasionally groups) to the event.
		// Previously ignored, leaving event_participants empty and the
		// "who took part in this event" lookup dead.
		if participants, ok := f["participants"].([]interface{}); ok {
			for _, pID := range participants {
				if s, ok := pID.(string); ok {
					tx.Exec("INSERT OR IGNORE INTO event_participants (event_id, participant_id) VALUES (?, ?)", id, s)
				}
			}
		}
	})

	// 7. Seed Groups (Fixed: Using groupName and adding memberships)
	seedFile(baseDir+"/peopleGroups.json", func(id string, f map[string]interface{}) {
		groupName := getString(f, "groupName") // Explicitly use groupName
		if groupName == "" {
			groupName = getString(f, "name") // Fallback just in case
		}

		tx.Exec("INSERT OR REPLACE INTO groups (id, name) VALUES (?, ?)", id, groupName)

		// 2. Seed Memberships (The link between People and Groups)
		if members, ok := f["members"].([]interface{}); ok {
			for _, pID := range members {
				// pID is the rec... ID of the person
				tx.Exec("INSERT OR IGNORE INTO group_memberships (group_id, person_id) VALUES (?, ?)",
					id, pID.(string))
			}
		}
	})

	tx.Commit()
}

// Generic loader for Theo-style JSON
func seedFile(path string, processor func(string, map[string]interface{})) {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("Skip file %s: %v", path, err)
		return
	}
	var items []TheoBase
	json.Unmarshal(data, &items)
	for _, item := range items {
		processor(item.ID, item.Fields)
	}
}

func SeedCrossReferences(db *sql.DB, filePath string) {
	tx, _ := db.Begin()
	file, err := os.Open(filePath)
	if err != nil {
		log.Fatalf("Failed to open cross refs: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	// Skip header line if it exists
	scanner.Scan()

	inserted := 0
	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "\t")
		if len(parts) < 2 {
			continue
		}

		// Source uses OSIS codes (Deut, Ps, Matt, 1Kgs, Song...) and ranges
		// (Col.1.16-Col.1.17). Route both sides through the canonical resolver
		// and expand ranges so all 66 books survive the from/to verse FKs.
		for _, from := range expandRefRange(parts[0]) {
			for _, to := range expandRefRange(parts[1]) {
				if res, err := tx.Exec("INSERT OR IGNORE INTO cross_references (from_verse, to_verse) VALUES (?, ?)", from, to); err == nil {
					if n, _ := res.RowsAffected(); n > 0 {
						inserted++
					}
				}
			}
		}
	}
	tx.Commit()
	fmt.Printf("✅ Cross-references seeded: %d links.\n", inserted)
}
