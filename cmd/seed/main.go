package main

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

func getBookCode(name string) string {
	normalized := normalizeBookName(name)
	bookMap := map[string]string{
		"Genesis": "GEN", "Exodus": "EXO", "Leviticus": "LEV", "Numbers": "NUM", "Deuteronomy": "DEU",
		"Joshua": "JOS", "Judges": "JDG", "Ruth": "RUT", "1 Samuel": "1SA", "2 Samuel": "2SA",
		"1 Kings": "1KI", "2 Kings": "2KI", "1 Chronicles": "1CH", "2 Chronicles": "2CH",
		"I Samuel": "1SA", "II Samuel": "2SA", "I Kings": "1KI", "II Kings": "2KI",
		"I Chronicles": "1CH", "II Chronicles": "2CH", "I Corinthians": "1CO", "II Corinthians": "2CO",
		"I Thessalonians": "1TH", "II Thessalonians": "2TH", "I Timothy": "1TI", "II Timothy": "2TI",
		"I Peter": "1PE", "II Peter": "2PE", "I John": "1JO", "II John": "2JO",
		"Ezra": "EZR", "Nehemiah": "NEH", "Esther": "EST", "Job": "JOB", "Psalms": "PSA",
		"Proverbs": "PRO", "Ecclesiastes": "ECC", "Song of Solomon": "SNG", "Isaiah": "ISA",
		"Jeremiah": "JER", "Lamentations": "LAM", "Ezekiel": "EZK", "Daniel": "DAN",
		"Hosea": "HOS", "Joel": "JOL", "Amos": "AMO", "Obadiah": "OBA", "Jonah": "JON",
		"Micah": "MIC", "Nahum": "NAM", "Habakkuk": "HAB", "Zephaniah": "ZEP", "Haggai": "HAG",
		"Zechariah": "ZEC", "Malachi": "MAL",
		"Matthew": "MAT", "Mark": "MRK", "Luke": "LUK", "John": "JHN", "Acts": "ACT",
		"Romans": "ROM", "1 Corinthians": "1CO", "2 Corinthians": "2CO", "Galatians": "GAL",
		"Ephesians": "EPH", "Philippians": "PHP", "Colossians": "COL", "1 Thessalonians": "1TH",
		"2 Thessalonians": "2TH", "1 Timothy": "1TI", "2 Timothy": "2TI", "Titus": "TIT",
		"Philemon": "PHM", "Hebrews": "HEB", "James": "JAS", "1 Peter": "1PE", "2 Peter": "2PE",
		"1 John": "1JO", "2 John": "2JO", "3 John": "3JO", "Jude": "JUD", "Revelation": "REV",
	}
	if code, ok := bookMap[normalized]; ok {
		return code
	}
	if code, ok := bookMap[name]; ok {
		return code
	}
	if len(normalized) >= 3 {
		return strings.ToUpper(normalized[:3])
	}
	return strings.ToUpper(normalized)
}

func normalizeBookName(name string) string {
	normalized := strings.TrimSpace(name)
	replacements := []struct{ old, new string }{
		{"I Samuel", "1 Samuel"},
		{"II Samuel", "2 Samuel"},
		{"I Kings", "1 Kings"},
		{"II Kings", "2 Kings"},
		{"I Chronicles", "1 Chronicles"},
		{"II Chronicles", "2 Chronicles"},
		{"I Corinthians", "1 Corinthians"},
		{"II Corinthians", "2 Corinthians"},
		{"I Thessalonians", "1 Thessalonians"},
		{"II Thessalonians", "2 Thessalonians"},
		{"I Timothy", "1 Timothy"},
		{"II Timothy", "2 Timothy"},
		{"I Peter", "1 Peter"},
		{"II Peter", "2 Peter"},
		{"I John", "1 John"},
		{"II John", "2 John"},
	}
	for _, replacement := range replacements {
		normalized = strings.ReplaceAll(normalized, replacement.old, replacement.new)
	}
	return normalized
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

func main() {
	dbPath := storage.ResolveDBPath("./data/bible.db")
	log.Printf("Using database: %s", dbPath)
	db := storage.InitDB(dbPath)
	defer db.Close()

	// Ensure constraints are managed during high-volume inserts
	db.Exec("PRAGMA foreign_keys = ON;")

	storage.CreateTables(db)

	seedBible(db, "./data/BSB.json")
	seedLexiconFolder(db, "./data/lexicons/")

	morphFiles := []string{
		"./data/morphology/TEGMC - Translators Expansion of Greek Morphhology Codes - STEPBible.org CC BY.txt",
		"./data/morphology/TEHMC - Translators Expansion of Hebrew Morphology Codes - STEPBible.org CC BY.txt",
	}
	for _, f := range morphFiles {
		seedMorphology(db, f)
	}

	seedAmalgamated(db, "./data/amalgamated/")

	seedVersification(db, "./data/versification/TVTMS - Translators Versification Traditions with Methodology for Standardisation for Eng+Heb+Lat+Grk+Others - STEPBible.org CC BY.txt")

	SeedGeographySuite(db, "./data/geography")

	SeedTheographicData(db, "./data/history")

	SeedCrossReferences(db, "./data/crossreference/cross_references.txt")

	fmt.Println("\n🚀 Full Scholarly Suite Seeded Successfully!")
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
		bookName := normalizeBookName(book.Name)
		bookCode := getBookCode(bookName)
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
	stmt, _ := tx.Prepare(`INSERT OR REPLACE INTO lexicon (strongs_id, word, transliteration, definition) VALUES (?, ?, ?, ?)`)
	scanner := bufio.NewScanner(file)

	inData := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "eStrong") {
			inData = true
			continue
		}
		if !inData || strings.HasPrefix(line, "===") || strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) >= 8 {
			sID := normalizeStrongs(parts[0])
			stmt.Exec(sID, parts[3], parts[4], SanitizeLexicon(parts[7]))
		}
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

	tx, _ := db.Begin()
	stmt, _ := tx.Prepare(`
        INSERT OR REPLACE INTO verse_analysis 
        (verse_id, word_order, surface_word, english_gloss, strongs_id, morph_code, manuscript_type) 
        VALUES (?, ?, ?, ?, ?, ?, ?)`)

	totalWords := 0
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
			if len(parts) < 5 {
				continue
			}

			// --- Clean Reference ---
			rawRef := parts[0]
			refSplit := strings.Split(rawRef, "#")
			verseID := strings.ToUpper(strings.TrimSpace(refSplit[0]))

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

			// --- Execute with Transaction-level Lazy Seeding ---
			_, err := stmt.Exec(verseID, wordOrder, surfaceWord, gloss, strongsID, morphCode, mType)
			if err != nil && strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
				// Use 'tx.Exec' so the current transaction sees these new records immediately
				tx.Exec("INSERT OR IGNORE INTO verses (id, text) VALUES (?, ?)", verseID, "[Text pending...]")
				if strongsID != "" {
					tx.Exec("INSERT OR IGNORE INTO lexicon (strongs_id, word) VALUES (?, ?)", strongsID, "[Definition pending...]")
				}
				// Retry
				_, err = stmt.Exec(verseID, wordOrder, surfaceWord, gloss, strongsID, morphCode, mType)
			}

			if err == nil {
				totalWords++
			}
		}
		file.Close()
	}
	tx.Commit()
	fmt.Printf("✅ Analysis seeded: %d tokens.\n", totalWords)
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

// Helper to turn "Exo.37:1-3" into "EXO.37.1" (taking the start of the range)
func normalizeRef(ref string) string {
	ref = strings.ToUpper(strings.TrimSpace(ref))
	if strings.Contains(ref, "-") {
		ref = strings.Split(ref, "-")[0]
	}
	// Handle both Colons and spaces to ensure "MAT 1:1" becomes "MAT.1.1"
	ref = strings.ReplaceAll(ref, ":", ".")
	ref = strings.ReplaceAll(ref, " ", ".")
	return ref
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

	// 6. Seed Events (Fixed: Adding event_verses bridge)
	seedFile(baseDir+"/events.json", func(id string, f map[string]interface{}) {
		tx.Exec("INSERT OR REPLACE INTO events (id, title, start_date, duration, sort_key) VALUES (?, ?, ?, ?, ?)",
			id, getString(f, "title"), getString(f, "startDate"), getString(f, "duration"), f["sortKey"])

		if verses, ok := f["verses"].([]interface{}); ok {
			for _, vID := range verses {
				tx.Exec("INSERT OR IGNORE INTO event_verses (event_id, verse_id) VALUES (?, ?)", id, vID)
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

	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "\t")
		if len(parts) < 2 {
			continue
		}

		// Normalize to UPPERCASE to match your 'verses' table IDs
		from := strings.ToUpper(parts[0])
		to := strings.ToUpper(parts[1])

		tx.Exec("INSERT OR IGNORE INTO cross_references (from_verse, to_verse) VALUES (?, ?)", from, to)
	}
	tx.Commit()
	log.Println("Cross-references seeded.")
}
