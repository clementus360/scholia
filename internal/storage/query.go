package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

func resolveCanonicalLocationID(db *sql.DB, locationID string) (string, error) {
	locationID = strings.TrimSpace(locationID)
	if locationID == "" {
		return "", nil
	}

	var canonicalID string
	err := db.QueryRow("SELECT canonical_location_id FROM location_aliases WHERE alias_id = ? LIMIT 1", locationID).Scan(&canonicalID)
	if err == sql.ErrNoRows {
		return locationID, nil
	}
	if err != nil {
		return "", err
	}
	canonicalID = strings.TrimSpace(canonicalID)
	if canonicalID == "" {
		return locationID, nil
	}
	return canonicalID, nil
}

var bsbCodeToTheoBook = map[string]string{
	"GEN": "Gen", "EXO": "Exod", "LEV": "Lev", "NUM": "Num", "DEU": "Deut",
	"JOS": "Josh", "JDG": "Judg", "RUT": "Ruth", "1SA": "1Sam", "2SA": "2Sam",
	"1KI": "1Kgs", "2KI": "2Kgs", "1CH": "1Chr", "2CH": "2Chr",
	"EZR": "Ezra", "NEH": "Neh", "EST": "Esth", "JOB": "Job", "PSA": "Ps",
	"PRO": "Prov", "ECC": "Eccl", "SNG": "Song", "ISA": "Isa", "JER": "Jer",
	"LAM": "Lam", "EZK": "Ezek", "DAN": "Dan", "HOS": "Hos", "JOL": "Joel",
	"AMO": "Amos", "OBA": "Obad", "JON": "Jonah", "MIC": "Mic", "NAM": "Nah",
	"HAB": "Hab", "ZEP": "Zeph", "HAG": "Hag", "ZEC": "Zech", "MAL": "Mal",
	"MAT": "Matt", "MRK": "Mark", "LUK": "Luke", "JHN": "John", "ACT": "Acts",
	"ROM": "Rom", "1CO": "1Cor", "2CO": "2Cor", "GAL": "Gal", "EPH": "Eph",
	"PHP": "Phil", "COL": "Col", "1TH": "1Thess", "2TH": "2Thess", "1TI": "1Tim",
	"2TI": "2Tim", "TIT": "Titus", "PHM": "Phlm", "HEB": "Heb", "JAS": "Jas",
	"1PE": "1Pet", "2PE": "2Pet", "1JO": "1John", "2JO": "2John", "3JO": "3John",
	"JUD": "Jude", "REV": "Rev",
}

type LexiconEntry struct {
	StrongsID       string `json:"strongs_id"`
	Word            string `json:"word"`
	Transliteration string `json:"transliteration"`
	Definition      string `json:"definition"`
}

type MorphologyEntry struct {
	Code     string `json:"code"`
	ShortDef string `json:"short_def"`
	LongExp  string `json:"long_exp"`
}

type VerseAnalysisToken struct {
	WordOrder      int              `json:"word_order"`
	SurfaceWord    string           `json:"surface_word"`
	EnglishGloss   string           `json:"english_gloss"`
	StrongsID      string           `json:"strongs_id"`
	MorphCode      string           `json:"morph_code"`
	ManuscriptType string           `json:"manuscript_type"`
	Lexicon        *LexiconEntry    `json:"lexicon,omitempty"`
	Morphology     *MorphologyEntry `json:"morphology,omitempty"`
}

type Location struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	ModernName   string   `json:"modern_name"`
	Latitude     *float64 `json:"latitude,omitempty"`
	Longitude    *float64 `json:"longitude,omitempty"`
	FeatureType  string   `json:"feature_type"`
	GeometryType string   `json:"geometry_type"`
	ImageFile    string   `json:"image_file"`
	ImageURL     string   `json:"image_url"`
	CreditURL    string   `json:"credit_url"`
	ImageAuthor  string   `json:"image_author"`
	SourceInfo   string   `json:"source_info"`
	// Geometry is a JSON object describing the map shape:
	// { kind, land_or_water, boundary:[[lon,lat]...], label_line:[...], external_file }.
	// Emitted as a nested object (not a string); omitted when absent.
	Geometry json.RawMessage `json:"geometry,omitempty"`
}

// PersonRelation is one tie in the family graph, already resolved to a name so
// the caller does not have to fetch every relative separately.
type PersonRelation struct {
	Relation string `json:"relation"` // father | mother | child | sibling | partner
	ID       string `json:"id"`
	Name     string `json:"name"`
}

type Person struct {
	ID             string           `json:"id"`
	Name           string           `json:"name"`
	LookupName     string           `json:"lookup_name"`
	Gender         string           `json:"gender"`
	BirthYear      int              `json:"birth_year"`
	DeathYear      int              `json:"death_year"`
	DictionaryText string           `json:"dictionary_text"`
	Slug           string           `json:"slug"`
	AlsoCalled     string           `json:"also_called,omitempty"`
	BirthPlace     string           `json:"birth_place,omitempty"`
	DeathPlace     string           `json:"death_place,omitempty"`
	Relations      []PersonRelation `json:"relations,omitempty"`
}

type Group struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// EventRef names a neighbouring event without pulling its whole record.
type EventRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// EventPlace is where an event happened.
type EventPlace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Event struct {
	ID        string       `json:"id"`
	Title     string       `json:"title"`
	StartDate string       `json:"start_date"`
	Duration  string       `json:"duration"`
	SortKey   float64      `json:"sort_key"`
	Notes     string       `json:"notes,omitempty"`
	PartOf    *EventRef    `json:"part_of,omitempty"`
	Follows   *EventRef    `json:"follows,omitempty"`
	Locations []EventPlace `json:"locations,omitempty"`
}

// Era is one band of the traditional chronology a verse falls into.
type Era struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	StartYear int    `json:"start_year"`
	EndYear   int    `json:"end_year"`
	Summary   string `json:"summary"`
}

// BookSetting is the historical framing of the book a verse sits in.
type BookSetting struct {
	Name         string   `json:"name"`
	Division     string   `json:"division,omitempty"`
	Testament    string   `json:"testament,omitempty"`
	YearWritten  string   `json:"year_written,omitempty"`
	PlaceWritten string   `json:"place_written,omitempty"`
	Writers      []string `json:"writers,omitempty"`
}

// VerseSetting answers "when and where am I" for a single verse. Every field is
// optional: the dataset dates about 90% of verses and names a writing place for
// only a handful of books.
type VerseSetting struct {
	YearNum *int         `json:"year_num,omitempty"`
	Era     *Era         `json:"era,omitempty"`
	Book    *BookSetting `json:"book,omitempty"`
	// "verse" when the era comes from this verse's own year, "book" when the
	// verse is undated and the era was inferred from the rest of its book.
	EraSource string `json:"era_source,omitempty"`
}

// WorldRuler is a foreign ruler in office during the passage's era.
type WorldRuler struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Title     string `json:"title"`
	Region    string `json:"region"`
	StartYear *int   `json:"start_year,omitempty"`
	EndYear   *int   `json:"end_year,omitempty"`
	Note      string `json:"note,omitempty"`
	// True when the passage's own year falls inside this reign. Only set where
	// the two chronologies agree — see GetWorldContext.
	Current bool `json:"current"`
}

// WorldEvent is something happening elsewhere in the same era.
type WorldEvent struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Region  string `json:"region"`
	Year    *int   `json:"year,omitempty"`
	Summary string `json:"summary,omitempty"`
	// True when the event falls within 25 years of the passage, on the same
	// chronology-agreement rule as WorldRuler.Current.
	Nearby bool `json:"nearby"`
}

// EraBackground is a short written piece on one region during one era.
type EraBackground struct {
	ID     string `json:"id"`
	Region string `json:"region"`
	Title  string `json:"title"`
	Body   string `json:"body"`
}

// WorldContext is the world outside the passage: who was ruling the
// surrounding powers, what was happening in them, and the background to it.
type WorldContext struct {
	EraID   string `json:"era_id"`
	EraName string `json:"era_name"`
	// True when the passage's year can be compared with these dates directly.
	// False for the Old Testament, where the corpus uses a traditional
	// chronology and these dates use the conventional one.
	YearAligned bool            `json:"year_aligned"`
	Rulers      []WorldRuler    `json:"rulers"`
	Events      []WorldEvent    `json:"events"`
	Backgrounds []EraBackground `json:"backgrounds"`
}

// ExternalArticle is one encyclopedia article on the world around a passage,
// quoted from its source rather than summarised.
//
// Extract is the article's own opening text. Revision and Retrieved pin it to
// the version quoted, so the citation stays checkable after the live page moves
// on. Relevance is the one clause saying why it was attached to this passage —
// the only field here that came out of the harvest rather than the source, and
// the UI labels it as such.
type ExternalArticle struct {
	ID          string `json:"id"`
	WikidataID  string `json:"wikidata_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Extract     string `json:"extract"`
	URL         string `json:"url"`
	Revision    int64  `json:"revision"`
	Retrieved   string `json:"retrieved"`
	License     string `json:"license"`
	Relevance   string `json:"relevance"`
	// "event" when the article is tied to the episode this verse belongs to,
	// "book" when it is background for the book as a whole. The UI leans on
	// this to say how specific the connection is.
	Scope string `json:"scope"`
	// "history" for the world the passage sits in, "parallel" for another
	// ancient text that resembles it. The UI keeps the two apart and disclaims
	// the second; see the schema comment on external_article_links.
	Kind string `json:"kind"`
}

// GetExternalContext returns the sourced background for a verse: articles
// attached to the events this verse belongs to, then articles attached to its
// book.
//
// Event articles come first and are the point of the feature — they are as
// specific as the corpus can get, because an event already knows its own
// verses. Book articles are the fallback for the poetry, proverbs and letters
// that no dated event covers.
//
// A verse in two overlapping events can reach the same article twice, so the
// scan keeps the first occurrence and drops the rest; ordering by scope first
// means the copy that survives is the more specific one.
func GetExternalContext(db *sql.DB, verseID string) ([]ExternalArticle, error) {
	// The corpus reaches a verse under several ids — the BSB code, the
	// Theographic OSIS ref and the record id — and event_verses is keyed by
	// the last of those. getVerseLookupKeys is what every other verse-joined
	// query here uses to bridge them.
	keys, err := getVerseLookupKeys(db, verseID)
	if err != nil {
		return nil, err
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(keys)), ",")
	query := fmt.Sprintf(`
		SELECT a.id, a.wikidata_id, a.title, COALESCE(a.description, ''),
		       COALESCE(a.extract, ''), COALESCE(a.url, ''), COALESCE(a.revision, 0),
		       COALESCE(a.retrieved, ''), COALESCE(a.license, ''),
		       COALESCE(l.relevance, ''), l.scope, COALESCE(l.kind, 'parallel'), l.rank
		FROM external_article_links l
		JOIN external_articles a ON a.id = l.article_id
		WHERE (l.scope = 'event' AND l.target_id IN (
		          SELECT ev.event_id FROM event_verses ev WHERE ev.verse_id IN (%[1]s)))
		   OR (l.scope = 'book' AND l.target_id IN (
		          SELECT b.id FROM books b
		          JOIN verses v ON lower(v.book) = lower(b.book_name)
		          WHERE v.id IN (%[1]s)))
		ORDER BY CASE l.kind WHEN 'history' THEN 0 ELSE 1 END,
		         CASE l.scope WHEN 'event' THEN 0 ELSE 1 END,
		         l.rank ASC, a.title ASC`,
		placeholders)

	// The key list is interpolated twice, so every argument is passed twice.
	args := make([]any, 0, len(keys)*2)
	for range 2 {
		for _, key := range keys {
			args = append(args, key)
		}
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var (
		articles []ExternalArticle
		seen     = map[string]bool{}
	)

	for rows.Next() {
		var a ExternalArticle
		var rank int

		if err := rows.Scan(&a.ID, &a.WikidataID, &a.Title, &a.Description, &a.Extract,
			&a.URL, &a.Revision, &a.Retrieved, &a.License, &a.Relevance, &a.Scope, &a.Kind, &rank); err != nil {
			return nil, err
		}

		if seen[a.ID] {
			continue
		}

		seen[a.ID] = true
		articles = append(articles, a)
	}

	return articles, rows.Err()
}

// DictionaryArticle is one public-domain reference article attached to a verse
// through the people and places it mentions.
type DictionaryArticle struct {
	ID     string `json:"id"`
	Term   string `json:"term"`
	Body   string `json:"body"`
	Source string `json:"source"`
	Kind   string `json:"kind"` // person | place
}

func GetVerseByID(db *sql.DB, osisID string) (*Verse, error) {
	verse := &Verse{ID: osisID}
	var id, translation, book, text sql.NullString
	var chapter, verseNum sql.NullInt64
	err := db.QueryRow(
		"SELECT id, translation, book, chapter, verse, text FROM verses WHERE id = ?",
		osisID,
	).Scan(&id, &translation, &book, &chapter, &verseNum, &text)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if id.Valid {
		verse.ID = id.String
	}
	if translation.Valid {
		verse.Translation = translation.String
	}
	if book.Valid {
		verse.Book = book.String
	}
	if chapter.Valid {
		verse.Chapter = int(chapter.Int64)
	}
	if verseNum.Valid {
		verse.Verse = int(verseNum.Int64)
	}
	if text.Valid {
		verse.Text = text.String
	}

	return verse, nil
}

func GetVerseAnalysisByVerseID(db *sql.DB, verseID string) ([]VerseAnalysisToken, error) {
	resolvedVerseID, err := resolveVerseAnalysisVerseID(db, verseID)
	if err != nil {
		return nil, err
	}
	if resolvedVerseID == "" {
		return []VerseAnalysisToken{}, nil
	}

	rows, err := db.Query(`
		SELECT
			va.word_order,
			va.surface_word,
			va.english_gloss,
			COALESCE(va.strongs_id, ''),
			COALESCE(va.morph_code, ''),
			COALESCE(va.manuscript_type, ''),
			COALESCE(l.strongs_id, ''),
			COALESCE(l.word, ''),
			COALESCE(l.transliteration, ''),
			COALESCE(l.definition, ''),
			COALESCE(m.code, ''),
			COALESCE(m.short_def, ''),
			COALESCE(m.long_exp, '')
		FROM verse_analysis va
		LEFT JOIN lexicon l ON l.strongs_id = va.strongs_id
		LEFT JOIN morphology m ON m.code = va.morph_code
		WHERE va.verse_id = ?
		ORDER BY va.word_order ASC`, resolvedVerseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tokens := make([]VerseAnalysisToken, 0)
	for rows.Next() {
		var token VerseAnalysisToken
		var lexiconID, lexiconWord, lexiconTransliteration, lexiconDefinition string
		var morphCode, morphShort, morphLong string
		if err := rows.Scan(
			&token.WordOrder,
			&token.SurfaceWord,
			&token.EnglishGloss,
			&token.StrongsID,
			&token.MorphCode,
			&token.ManuscriptType,
			&lexiconID,
			&lexiconWord,
			&lexiconTransliteration,
			&lexiconDefinition,
			&morphCode,
			&morphShort,
			&morphLong,
		); err != nil {
			return nil, err
		}

		if lexiconID != "" {
			token.Lexicon = &LexiconEntry{
				StrongsID:       lexiconID,
				Word:            lexiconWord,
				Transliteration: lexiconTransliteration,
				Definition:      lexiconDefinition,
			}
		}
		if morphCode != "" {
			token.Morphology = &MorphologyEntry{
				Code:     morphCode,
				ShortDef: morphShort,
				LongExp:  morphLong,
			}
		}
		tokens = append(tokens, token)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return tokens, nil
}

func resolveVerseAnalysisVerseID(db *sql.DB, rawVerseID string) (string, error) {
	candidates := verseAnalysisCandidates(rawVerseID)
	if len(candidates) == 0 {
		return "", nil
	}

	for _, candidate := range candidates {
		var count int
		if err := db.QueryRow("SELECT COUNT(1) FROM verse_analysis WHERE verse_id = ?", candidate).Scan(&count); err != nil {
			return "", err
		}
		if count > 0 {
			return candidate, nil
		}
	}

	for _, candidate := range candidates {
		if strings.Contains(candidate, "(") {
			continue
		}
		var matched string
		err := db.QueryRow("SELECT verse_id FROM verse_analysis WHERE verse_id LIKE ? LIMIT 1", candidate+"(%").Scan(&matched)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return "", err
		}
		matched = strings.TrimSpace(matched)
		if matched != "" {
			return matched, nil
		}
	}

	return "", nil
}

func verseAnalysisCandidates(raw string) []string {
	raw = strings.ToUpper(strings.TrimSpace(raw))
	if raw == "" {
		return nil
	}

	candidates := make([]string, 0, 4)
	seen := map[string]struct{}{}
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		candidates = append(candidates, v)
	}

	add(raw)

	open := strings.Index(raw, "(")
	close := strings.LastIndex(raw, ")")
	if open > 0 && close > open {
		base := strings.TrimSpace(raw[:open])
		inside := strings.TrimSpace(raw[open+1 : close])
		add(base)

		if inside != "" {
			add(inside)
			if strings.Count(inside, ".") == 1 {
				if dot := strings.Index(base, "."); dot > 0 {
					book := strings.TrimSpace(base[:dot])
					if book != "" {
						add(book + "." + inside)
					}
				}
			}
		}
	}

	return candidates
}

func GetLexiconByID(db *sql.DB, strongsID string) (*LexiconEntry, error) {
	entry := &LexiconEntry{}
	err := db.QueryRow(
		"SELECT strongs_id, word, transliteration, definition FROM lexicon WHERE strongs_id = ?",
		strongsID,
	).Scan(&entry.StrongsID, &entry.Word, &entry.Transliteration, &entry.Definition)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return entry, nil
}

func GetLocationByID(db *sql.DB, locationID string) (*Location, error) {
	canonicalID, err := resolveCanonicalLocationID(db, locationID)
	if err != nil {
		return nil, err
	}

	row := db.QueryRow(`
		SELECT id, name, modern_name, latitude, longitude, feature_type, geometry_type, image_file, image_url, credit_url, image_author, source_info, geometry
		FROM locations WHERE id = ?`, canonicalID)

	var location Location
	var latitude, longitude sql.NullFloat64
	var modernName, featureType, geometryType, imageFile, imageURL, creditURL, imageAuthor, sourceInfo, geometry sql.NullString
	if err := row.Scan(
		&location.ID,
		&location.Name,
		&modernName,
		&latitude,
		&longitude,
		&featureType,
		&geometryType,
		&imageFile,
		&imageURL,
		&creditURL,
		&imageAuthor,
		&sourceInfo,
		&geometry,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	if geometry.Valid && geometry.String != "" {
		location.Geometry = json.RawMessage(geometry.String)
	}
	if modernName.Valid {
		location.ModernName = modernName.String
	}
	if featureType.Valid {
		location.FeatureType = featureType.String
	}
	if geometryType.Valid {
		location.GeometryType = geometryType.String
	}
	if imageFile.Valid {
		location.ImageFile = imageFile.String
	}
	if imageURL.Valid {
		location.ImageURL = imageURL.String
	}
	if creditURL.Valid {
		location.CreditURL = creditURL.String
	}
	if imageAuthor.Valid {
		location.ImageAuthor = imageAuthor.String
	}
	if sourceInfo.Valid {
		location.SourceInfo = sourceInfo.String
	}

	if latitude.Valid {
		value := latitude.Float64
		location.Latitude = &value
	}
	if longitude.Valid {
		value := longitude.Float64
		location.Longitude = &value
	}

	return &location, nil
}

func GetLocationsByVerseID(db *sql.DB, verseID string) ([]Location, error) {
	keys, err := getVerseLookupKeys(db, verseID)
	if err != nil {
		return nil, err
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(keys)), ",")
	query := fmt.Sprintf(`
		SELECT l.id, l.name, l.modern_name, l.latitude, l.longitude, l.feature_type, l.geometry_type, l.image_file, l.image_url, l.credit_url, l.image_author, l.source_info, l.geometry
		FROM locations l
		INNER JOIN verse_locations vl ON vl.location_id = l.id
		WHERE vl.verse_id IN (%s)
		ORDER BY l.name ASC`, placeholders)

	args := make([]any, 0, len(keys))
	for _, key := range keys {
		args = append(args, key)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	locations := make([]Location, 0)
	for rows.Next() {
		var location Location
		var latitude, longitude sql.NullFloat64
		var modernName, featureType, geometryType, imageFile, imageURL, creditURL, imageAuthor, sourceInfo, geometry sql.NullString
		if err := rows.Scan(
			&location.ID,
			&location.Name,
			&modernName,
			&latitude,
			&longitude,
			&featureType,
			&geometryType,
			&imageFile,
			&imageURL,
			&creditURL,
			&imageAuthor,
			&sourceInfo,
			&geometry,
		); err != nil {
			return nil, err
		}
		if geometry.Valid && geometry.String != "" {
			location.Geometry = json.RawMessage(geometry.String)
		}
		if modernName.Valid {
			location.ModernName = modernName.String
		}
		if featureType.Valid {
			location.FeatureType = featureType.String
		}
		if geometryType.Valid {
			location.GeometryType = geometryType.String
		}
		if imageFile.Valid {
			location.ImageFile = imageFile.String
		}
		if imageURL.Valid {
			location.ImageURL = imageURL.String
		}
		if creditURL.Valid {
			location.CreditURL = creditURL.String
		}
		if imageAuthor.Valid {
			location.ImageAuthor = imageAuthor.String
		}
		if sourceInfo.Valid {
			location.SourceInfo = sourceInfo.String
		}
		if latitude.Valid {
			value := latitude.Float64
			location.Latitude = &value
		}
		if longitude.Valid {
			value := longitude.Float64
			location.Longitude = &value
		}
		locations = append(locations, location)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return dedupeLocations(locations), nil
}

func dedupeLocations(locations []Location) []Location {
	if len(locations) <= 1 {
		return locations
	}

	indexByKey := make(map[string]int)
	deduped := make([]Location, 0, len(locations))

	for _, loc := range locations {
		key := locationIdentityKey(loc)
		if idx, exists := indexByKey[key]; exists {
			if locationQuality(loc) > locationQuality(deduped[idx]) {
				deduped[idx] = loc
			}
			continue
		}
		indexByKey[key] = len(deduped)
		deduped = append(deduped, loc)
	}

	return deduped
}

func locationIdentityKey(loc Location) string {
	name := strings.ToLower(strings.TrimSpace(loc.Name))
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.ReplaceAll(name, "_", " ")
	name = strings.Join(strings.Fields(name), " ")
	featureType := strings.ToLower(strings.TrimSpace(loc.FeatureType))
	if name == "" {
		return strings.ToLower(strings.TrimSpace(loc.ID))
	}
	return name + "|" + featureType
}

func locationQuality(loc Location) int {
	score := 0
	if loc.Latitude != nil || loc.Longitude != nil {
		score += 3
	}
	if strings.TrimSpace(loc.ModernName) != "" {
		score += 2
	}
	if strings.TrimSpace(loc.GeometryType) != "" {
		score += 1
	}
	if strings.TrimSpace(loc.ImageURL) != "" {
		score += 1
	}
	if strings.TrimSpace(loc.SourceInfo) != "" {
		score += 1
	}
	return score
}

func GetPeopleByVerseID(db *sql.DB, verseID string) ([]Person, error) {
	keys, err := getVerseLookupKeys(db, verseID)
	if err != nil {
		return nil, err
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(keys)), ",")
	query := fmt.Sprintf(`
		SELECT p.id, p.name, p.lookup_name, p.gender, p.birth_year, p.death_year, p.dictionary_text, p.slug,
		       COALESCE(p.also_called, ''), COALESCE(bp.name, ''), COALESCE(dp.name, '')
		FROM people p
		INNER JOIN person_verses pv ON pv.person_id = p.id
		LEFT JOIN locations bp ON bp.id = p.birth_place_id
		LEFT JOIN locations dp ON dp.id = p.death_place_id
		WHERE pv.verse_id IN (%s)
		ORDER BY p.name ASC`, placeholders)

	args := make([]any, 0, len(keys))
	for _, key := range keys {
		args = append(args, key)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	people := make([]Person, 0)
	for rows.Next() {
		var person Person
		if err := rows.Scan(&person.ID, &person.Name, &person.LookupName, &person.Gender, &person.BirthYear, &person.DeathYear, &person.DictionaryText, &person.Slug,
			&person.AlsoCalled, &person.BirthPlace, &person.DeathPlace); err != nil {
			return nil, err
		}
		people = append(people, person)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := attachPersonRelations(db, people); err != nil {
		return nil, err
	}
	return people, nil
}

// attachPersonRelations fills in the family graph for people already loaded.
// One query for the whole set rather than one per person: a verse can mention a
// dozen people, and each of those can have a dozen relatives.
func attachPersonRelations(db *sql.DB, people []Person) error {
	if len(people) == 0 {
		return nil
	}

	ids := make([]any, 0, len(people))
	index := make(map[string]int, len(people))
	for i, person := range people {
		ids = append(ids, person.ID)
		index[person.ID] = i
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	query := fmt.Sprintf(`
		SELECT pr.person_id, pr.relation, related.id, related.name
		FROM person_relations pr
		INNER JOIN people related ON related.id = pr.related_person_id
		WHERE pr.person_id IN (%s) AND COALESCE(related.name, '') <> ''
		ORDER BY pr.relation ASC, related.name ASC`, placeholders)

	rows, err := db.Query(query, ids...)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var personID string
		var relation PersonRelation
		if err := rows.Scan(&personID, &relation.Relation, &relation.ID, &relation.Name); err != nil {
			return err
		}
		if i, ok := index[personID]; ok {
			people[i].Relations = append(people[i].Relations, relation)
		}
	}
	return rows.Err()
}

func GetPersonByID(db *sql.DB, personID string) (*Person, error) {
	person := &Person{}
	err := db.QueryRow(`
		SELECT p.id, p.name, p.lookup_name, p.gender, p.birth_year, p.death_year, p.dictionary_text, p.slug,
		       COALESCE(p.also_called, ''), COALESCE(bp.name, ''), COALESCE(dp.name, '')
		FROM people p
		LEFT JOIN locations bp ON bp.id = p.birth_place_id
		LEFT JOIN locations dp ON dp.id = p.death_place_id
		WHERE p.id = ?`, personID).Scan(
		&person.ID, &person.Name, &person.LookupName, &person.Gender, &person.BirthYear, &person.DeathYear, &person.DictionaryText, &person.Slug,
		&person.AlsoCalled, &person.BirthPlace, &person.DeathPlace)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	single := []Person{*person}
	if err := attachPersonRelations(db, single); err != nil {
		return nil, err
	}
	return &single[0], nil
}

func GetGroupByID(db *sql.DB, groupID string) (*Group, error) {
	group := &Group{}
	err := db.QueryRow("SELECT id, name FROM groups WHERE id = ?", groupID).Scan(&group.ID, &group.Name)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return group, nil
}

func GetGroupsByVerseID(db *sql.DB, verseID string) ([]Group, error) {
	keys, err := getVerseLookupKeys(db, verseID)
	if err != nil {
		return nil, err
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(keys)), ",")
	query := fmt.Sprintf(`
		SELECT DISTINCT g.id, g.name
		FROM groups g
		INNER JOIN group_memberships gm ON gm.group_id = g.id
		INNER JOIN person_verses pv ON pv.person_id = gm.person_id
		WHERE pv.verse_id IN (%s)
		ORDER BY g.name ASC`, placeholders)

	args := make([]any, 0, len(keys))
	for _, key := range keys {
		args = append(args, key)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groups := make([]Group, 0)
	for rows.Next() {
		var group Group
		if err := rows.Scan(&group.ID, &group.Name); err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return groups, nil
}

func GetEventByID(db *sql.DB, eventID string) (*Event, error) {
	event := &Event{}
	err := db.QueryRow("SELECT id, title, start_date, duration, sort_key FROM events WHERE id = ?", eventID).Scan(
		&event.ID, &event.Title, &event.StartDate, &event.Duration, &event.SortKey)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return event, nil
}

func GetEventsByVerseID(db *sql.DB, verseID string) ([]Event, error) {
	keys, err := getVerseLookupKeys(db, verseID)
	if err != nil {
		return nil, err
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(keys)), ",")
	query := fmt.Sprintf(`
		SELECT e.id, e.title, e.start_date, e.duration, e.sort_key, COALESCE(e.notes, ''),
		       COALESCE(parent.id, ''), COALESCE(parent.title, ''),
		       COALESCE(prev.id, ''), COALESCE(prev.title, '')
		FROM events e
		INNER JOIN event_verses ev ON ev.event_id = e.id
		LEFT JOIN events parent ON parent.id = e.parent_event_id
		LEFT JOIN events prev ON prev.id = e.predecessor_event_id
		WHERE ev.verse_id IN (%s)
		ORDER BY e.sort_key ASC, e.title ASC`, placeholders)

	args := make([]any, 0, len(keys))
	for _, key := range keys {
		args = append(args, key)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]Event, 0)
	for rows.Next() {
		var event Event
		var parentID, parentTitle, prevID, prevTitle string
		if err := rows.Scan(&event.ID, &event.Title, &event.StartDate, &event.Duration, &event.SortKey, &event.Notes,
			&parentID, &parentTitle, &prevID, &prevTitle); err != nil {
			return nil, err
		}

		if parentTitle != "" {
			event.PartOf = &EventRef{ID: parentID, Title: parentTitle}
		}
		if prevTitle != "" {
			event.Follows = &EventRef{ID: prevID, Title: prevTitle}
		}

		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := attachEventLocations(db, events); err != nil {
		return nil, err
	}
	return events, nil
}

// attachEventLocations fills in where each event happened, in one query for the
// whole set.
func attachEventLocations(db *sql.DB, events []Event) error {
	if len(events) == 0 {
		return nil
	}

	ids := make([]any, 0, len(events))
	index := make(map[string]int, len(events))
	for i, event := range events {
		ids = append(ids, event.ID)
		index[event.ID] = i
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	query := fmt.Sprintf(`
		SELECT el.event_id, l.id, l.name
		FROM event_locations el
		INNER JOIN locations l ON l.id = el.location_id
		WHERE el.event_id IN (%s) AND COALESCE(l.name, '') <> ''
		ORDER BY l.name ASC`, placeholders)

	rows, err := db.Query(query, ids...)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var eventID string
		var place EventPlace
		if err := rows.Scan(&eventID, &place.ID, &place.Name); err != nil {
			return err
		}
		if i, ok := index[eventID]; ok {
			events[i].Locations = append(events[i].Locations, place)
		}
	}
	return rows.Err()
}

// GetVerseSetting returns the chronological and authorial framing of a verse:
// the year the dataset assigns it, the era that year falls in, and the book's
// own writing details.
func GetVerseSetting(db *sql.DB, verseID string) (*VerseSetting, error) {
	keys, err := getVerseLookupKeys(db, verseID)
	if err != nil {
		return nil, err
	}

	setting := &VerseSetting{}

	if len(keys) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(keys)), ",")
		args := make([]any, 0, len(keys))
		for _, key := range keys {
			args = append(args, key)
		}

		var year sql.NullInt64
		err := db.QueryRow(fmt.Sprintf(
			"SELECT year_num FROM verse_years WHERE osis_ref IN (%s) LIMIT 1", placeholders), args...).Scan(&year)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}

		if year.Valid {
			value := int(year.Int64)
			setting.YearNum = &value

			era := &Era{}
			err := db.QueryRow(`
				SELECT id, name, start_year, end_year, COALESCE(summary, '')
				FROM eras
				WHERE ? >= start_year AND ? < end_year
				ORDER BY sort_order ASC LIMIT 1`, value, value).Scan(
				&era.ID, &era.Name, &era.StartYear, &era.EndYear, &era.Summary)
			if err == nil {
				setting.Era = era
				setting.EraSource = "verse"
			} else if err != sql.ErrNoRows {
				return nil, err
			}
		}
	}

	book, err := getBookSettingForVerse(db, verseID)
	if err != nil {
		return nil, err
	}
	setting.Book = book

	// 3,078 verses carry no year of their own — Daniel 1:1 among them. Rather
	// than leave those with no setting at all, the era is taken from the rest of
	// the book, and labelled as such so the UI can say where it came from.
	if setting.Era == nil {
		era, err := inferEraFromBook(db, verseID)
		if err != nil {
			return nil, err
		}
		if era != nil {
			setting.Era = era
			setting.EraSource = "book"
		}
	}

	if setting.YearNum == nil && setting.Era == nil && setting.Book == nil {
		return nil, nil
	}
	return setting, nil
}

// inferEraFromBook picks the era most of a book's dated verses fall into. Used
// only for verses the dataset never dated.
func inferEraFromBook(db *sql.DB, verseID string) (*Era, error) {
	verse, err := GetVerseByID(db, verseID)
	if err != nil || verse == nil {
		return nil, err
	}

	var osisName string
	err = db.QueryRow(
		"SELECT osis_name FROM books WHERE lower(book_name) = lower(?) LIMIT 1", verse.Book).Scan(&osisName)
	if err == sql.ErrNoRows || osisName == "" {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	era := &Era{}
	err = db.QueryRow(`
		SELECT e.id, e.name, e.start_year, e.end_year, COALESCE(e.summary, '')
		FROM verse_years vy
		INNER JOIN eras e ON vy.year_num >= e.start_year AND vy.year_num < e.end_year
		WHERE vy.osis_ref LIKE ? || '.%'
		GROUP BY e.id
		ORDER BY COUNT(*) DESC LIMIT 1`, osisName).Scan(
		&era.ID, &era.Name, &era.StartYear, &era.EndYear, &era.Summary)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return era, nil
}

func getBookSettingForVerse(db *sql.DB, verseID string) (*BookSetting, error) {
	verse, err := GetVerseByID(db, verseID)
	if err != nil || verse == nil {
		return nil, err
	}

	var bookID string
	book := &BookSetting{}
	err = db.QueryRow(`
		SELECT b.id, b.book_name, COALESCE(b.division, ''), COALESCE(b.testament, ''),
		       COALESCE(b.year_written, ''), COALESCE(l.name, '')
		FROM books b
		LEFT JOIN locations l ON l.id = b.place_written_id
		WHERE lower(b.book_name) = lower(?) LIMIT 1`, verse.Book).Scan(
		&bookID, &book.Name, &book.Division, &book.Testament, &book.YearWritten, &book.PlaceWritten)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	rows, err := db.Query(`
		SELECT p.name FROM book_writers bw
		INNER JOIN people p ON p.id = bw.person_id
		WHERE bw.book_id = ? AND COALESCE(p.name, '') <> ''
		ORDER BY p.name ASC`, bookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		book.Writers = append(book.Writers, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return book, nil
}

// chronologyAlignedEras are the eras where the corpus's traditional dates and
// the conventional dates on the world data agree closely enough to compare a
// single year against a reign. Both rest on the same Roman records; everything
// earlier does not, and can differ by decades.
var chronologyAlignedEras = map[string]struct{}{
	"life-of-jesus": {},
	"early-church":  {},
}

// GetWorldContext returns the world outside the passage for the era it sits in:
// the foreign rulers of the period, events elsewhere, and the background pieces
// written for that era.
//
// The join is by era, not by year. See the schema comment on world_context for
// why. Where the two chronologies do agree, individual rows are additionally
// flagged as current or nearby so the UI can pick out the exact moment.
func GetWorldContext(db *sql.DB, verseID string) (*WorldContext, error) {
	setting, err := GetVerseSetting(db, verseID)
	if err != nil || setting == nil || setting.Era == nil {
		return nil, err
	}

	era := setting.Era
	_, aligned := chronologyAlignedEras[era.ID]

	world := &WorldContext{
		EraID:       era.ID,
		EraName:     era.Name,
		YearAligned: aligned && setting.YearNum != nil,
		Rulers:      []WorldRuler{},
		Events:      []WorldEvent{},
		Backgrounds: []EraBackground{},
	}

	rows, err := db.Query(`
		SELECT id, kind, name, COALESCE(title, ''), COALESCE(region, ''),
		       start_year, end_year, COALESCE(note, '')
		FROM world_context
		WHERE era_id = ?
		ORDER BY COALESCE(start_year, 0) ASC, name ASC`, era.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id, kind, name, title, region, note string
		var startYear, endYear sql.NullInt64

		if err := rows.Scan(&id, &kind, &name, &title, &region, &startYear, &endYear, &note); err != nil {
			return nil, err
		}

		start := nullableInt(startYear)
		end := nullableInt(endYear)

		switch kind {
		case "event":
			event := WorldEvent{ID: id, Title: name, Region: region, Year: start, Summary: note}
			if world.YearAligned && start != nil {
				event.Nearby = absInt(*start-*setting.YearNum) <= 25
			}
			world.Events = append(world.Events, event)
		default:
			ruler := WorldRuler{
				ID: id, Name: name, Title: title, Region: region,
				StartYear: start, EndYear: end, Note: note,
			}
			if world.YearAligned && start != nil && end != nil {
				ruler.Current = *start <= *setting.YearNum && *setting.YearNum <= *end
			}
			world.Rulers = append(world.Rulers, ruler)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	backgroundRows, err := db.Query(`
		SELECT id, COALESCE(region, ''), COALESCE(title, ''), COALESCE(body, '')
		FROM era_backgrounds WHERE era_id = ? ORDER BY sort_order ASC`, era.ID)
	if err != nil {
		return nil, err
	}
	defer backgroundRows.Close()

	for backgroundRows.Next() {
		var background EraBackground
		if err := backgroundRows.Scan(&background.ID, &background.Region, &background.Title, &background.Body); err != nil {
			return nil, err
		}
		world.Backgrounds = append(world.Backgrounds, background)
	}
	if err := backgroundRows.Err(); err != nil {
		return nil, err
	}

	if len(world.Rulers) == 0 && len(world.Events) == 0 && len(world.Backgrounds) == 0 {
		return nil, nil
	}
	return world, nil
}

func nullableInt(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	result := int(value.Int64)
	return &result
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

// articleStopWords are dictionary headwords common enough that matching them
// against a verse says nothing. Everything shorter than four letters is already
// excluded, so this only has to catch the frequent long ones.
var articleStopWords = map[string]struct{}{
	"they": {}, "them": {}, "their": {}, "with": {}, "from": {}, "that": {},
	"this": {}, "then": {}, "there": {}, "these": {}, "those": {}, "when": {},
	"unto": {}, "into": {}, "shall": {}, "said": {}, "come": {}, "went": {},
	"have": {}, "will": {}, "were": {}, "your": {}, "which": {}, "what": {},
}

// GetDictionaryArticlesByVerseID returns public-domain dictionary articles for
// the terms a verse actually uses — the Passover, leaven and firstborn kind of
// article, which no other part of the corpus carries.
//
// Articles about the verse's own people and places are deliberately excluded:
// those are already attached to the person and location records, and repeating
// them here would send the same several kilobytes of Easton twice.
func GetDictionaryArticlesByVerseID(db *sql.DB, verseID string, limit int) ([]DictionaryArticle, error) {
	if limit <= 0 {
		limit = 12
	}

	verse, err := GetVerseByID(db, verseID)
	if err != nil || verse == nil {
		return []DictionaryArticle{}, err
	}

	candidates := articleCandidateTerms(verse.Text)
	if len(candidates) == 0 {
		return []DictionaryArticle{}, nil
	}

	keys, err := getVerseLookupKeys(db, verseID)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		keys = []string{verseID}
	}

	termPlaceholders := strings.TrimSuffix(strings.Repeat("?,", len(candidates)), ",")
	keyPlaceholders := strings.TrimSuffix(strings.Repeat("?,", len(keys)), ",")

	// Longer headwords first: "unleavened bread" is a better hit than "bread",
	// and the limit should spend itself on the specific ones.
	query := fmt.Sprintf(`
		SELECT de.id, de.term, de.body, COALESCE(de.source, ''), COALESCE(de.match_type, '')
		FROM dictionary_entries de
		WHERE de.term_key IN (%s)
		  AND NOT EXISTS (
			SELECT 1 FROM dictionary_links dl
			WHERE dl.entry_id = de.id
			  AND ((dl.target_kind = 'person' AND dl.target_id IN (
					SELECT person_id FROM person_verses WHERE verse_id IN (%s)))
			    OR (dl.target_kind = 'place' AND dl.target_id IN (
					SELECT location_id FROM verse_locations WHERE verse_id IN (%s))))
		  )
		GROUP BY de.term_key
		ORDER BY length(de.term) DESC, de.term ASC
		LIMIT ?`, termPlaceholders, keyPlaceholders, keyPlaceholders)

	args := make([]any, 0, len(candidates)+len(keys)*2+1)
	for _, term := range candidates {
		args = append(args, term)
	}
	for range 2 {
		for _, key := range keys {
			args = append(args, key)
		}
	}
	args = append(args, limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	articles := make([]DictionaryArticle, 0)
	for rows.Next() {
		var article DictionaryArticle
		if err := rows.Scan(&article.ID, &article.Term, &article.Body, &article.Source, &article.Kind); err != nil {
			return nil, err
		}
		articles = append(articles, article)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return articles, nil
}

// articleCandidateTerms reduces a verse to the lowercased one-, two- and
// three-word phrases worth looking up as dictionary headwords.
func articleCandidateTerms(text string) []string {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && r != '\''
	})

	seen := map[string]struct{}{}
	candidates := make([]string, 0, len(words)*2)

	add := func(phrase string) {
		if len(phrase) < 4 {
			return
		}
		if _, stop := articleStopWords[phrase]; stop {
			return
		}
		if _, dup := seen[phrase]; dup {
			return
		}
		seen[phrase] = struct{}{}
		candidates = append(candidates, phrase)
	}

	for i, word := range words {
		add(word)

		// Easton lists most plurals under the singular headword, so a naive
		// de-pluralisation catches "priests" -> "priest" without a stemmer.
		if strings.HasSuffix(word, "es") {
			add(strings.TrimSuffix(word, "es"))
		}
		if strings.HasSuffix(word, "s") {
			add(strings.TrimSuffix(word, "s"))
		}

		if i+1 < len(words) {
			add(word + " " + words[i+1])
		}
		if i+2 < len(words) {
			add(word + " " + words[i+1] + " " + words[i+2])
		}
	}

	return candidates
}

func GetCrossReferencesByVerseID(db *sql.DB, verseID string, limit, offset int) ([]string, error) {
	keys, err := getVerseLookupKeys(db, verseID)
	if err != nil {
		return nil, err
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(keys)), ",")
	query := fmt.Sprintf("SELECT to_verse FROM cross_references WHERE from_verse IN (%s) ORDER BY to_verse ASC LIMIT ? OFFSET ?", placeholders)

	args := make([]any, 0, len(keys))
	for _, key := range keys {
		args = append(args, key)
	}
	args = append(args, limit, offset)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	refs := make([]string, 0)
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return refs, nil
}

func getVerseLookupKeys(db *sql.DB, verseID string) ([]string, error) {
	keySet := map[string]struct{}{}
	add := func(k string) {
		if k == "" {
			return
		}
		keySet[k] = struct{}{}
	}

	add(verseID)
	add(strings.ToUpper(verseID))

	parts := strings.Split(verseID, ".")
	if len(parts) == 3 {
		if theoBook, ok := bsbCodeToTheoBook[strings.ToUpper(parts[0])]; ok {
			theoRef := fmt.Sprintf("%s.%s.%s", theoBook, parts[1], parts[2])
			add(theoRef)
			add(strings.ToUpper(theoRef))
		}
	}

	verse, err := GetVerseByID(db, verseID)
	if err != nil {
		return nil, err
	}
	if verse != nil {
		var osisName sql.NullString
		if err := db.QueryRow("SELECT osis_name FROM books WHERE lower(book_name) = lower(?) LIMIT 1", verse.Book).Scan(&osisName); err == nil && osisName.Valid {
			theoRef := fmt.Sprintf("%s.%d.%d", osisName.String, verse.Chapter, verse.Verse)
			add(theoRef)
			add(strings.ToUpper(theoRef))
		}
	}

	baseKeys := make([]string, 0, len(keySet))
	for k := range keySet {
		baseKeys = append(baseKeys, k)
	}

	for _, key := range baseKeys {
		rows, err := db.Query("SELECT rec_id FROM verse_id_map WHERE osis_ref = ?", key)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var recID string
			if err := rows.Scan(&recID); err != nil {
				rows.Close()
				return nil, err
			}
			add(recID)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}

	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		keys = append(keys, verseID)
	}
	return keys, nil
}
