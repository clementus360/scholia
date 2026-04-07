package storage

import (
	"database/sql"
	"fmt"
	"strings"
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
}

type Person struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	LookupName     string `json:"lookup_name"`
	Gender         string `json:"gender"`
	BirthYear      int    `json:"birth_year"`
	DeathYear      int    `json:"death_year"`
	DictionaryText string `json:"dictionary_text"`
	Slug           string `json:"slug"`
}

type Group struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Event struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	StartDate string  `json:"start_date"`
	Duration  string  `json:"duration"`
	SortKey   float64 `json:"sort_key"`
}

type Note struct {
	ID            int64    `json:"id"`
	OwnerUserID   string   `json:"-"`
	Title         string   `json:"title"`
	MainReference string   `json:"main_reference"`
	Content       string   `json:"content"`
	VerseIDs      []string `json:"verse_ids,omitempty"`
	CreatedAt     string   `json:"created_at,omitempty"`
	UpdatedAt     string   `json:"updated_at,omitempty"`
}

type queryExecutor interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
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
		ORDER BY va.word_order ASC`, verseID)
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
		SELECT id, name, modern_name, latitude, longitude, feature_type, geometry_type, image_file, image_url, credit_url, image_author, source_info
		FROM locations WHERE id = ?`, canonicalID)

	var location Location
	var latitude, longitude sql.NullFloat64
	var modernName, featureType, geometryType, imageFile, imageURL, creditURL, imageAuthor, sourceInfo sql.NullString
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
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
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
		SELECT l.id, l.name, l.modern_name, l.latitude, l.longitude, l.feature_type, l.geometry_type, l.image_file, l.image_url, l.credit_url, l.image_author, l.source_info
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
		var modernName, featureType, geometryType, imageFile, imageURL, creditURL, imageAuthor, sourceInfo sql.NullString
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
		); err != nil {
			return nil, err
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
		SELECT p.id, p.name, p.lookup_name, p.gender, p.birth_year, p.death_year, p.dictionary_text, p.slug
		FROM people p
		INNER JOIN person_verses pv ON pv.person_id = p.id
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
		if err := rows.Scan(&person.ID, &person.Name, &person.LookupName, &person.Gender, &person.BirthYear, &person.DeathYear, &person.DictionaryText, &person.Slug); err != nil {
			return nil, err
		}
		people = append(people, person)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return people, nil
}

func GetPersonByID(db *sql.DB, personID string) (*Person, error) {
	person := &Person{}
	err := db.QueryRow(`
		SELECT id, name, lookup_name, gender, birth_year, death_year, dictionary_text, slug
		FROM people WHERE id = ?`, personID).Scan(
		&person.ID, &person.Name, &person.LookupName, &person.Gender, &person.BirthYear, &person.DeathYear, &person.DictionaryText, &person.Slug)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return person, nil
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
		SELECT e.id, e.title, e.start_date, e.duration, e.sort_key
		FROM events e
		INNER JOIN event_verses ev ON ev.event_id = e.id
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
		if err := rows.Scan(&event.ID, &event.Title, &event.StartDate, &event.Duration, &event.SortKey); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
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

func GetNotesByVerseID(db *sql.DB, ownerID, verseID string, limit, offset int) ([]Note, error) {
	if strings.TrimSpace(ownerID) == "" {
		return []Note{}, nil
	}
	rows, err := db.Query(`
		SELECT n.id, n.owner_user_id, n.title, n.main_reference, n.content, n.created_at, n.updated_at
		FROM notes n
		INNER JOIN note_verses nv ON nv.note_id = n.id
		WHERE nv.verse_id = ? AND n.owner_user_id = ?
		ORDER BY n.updated_at DESC, n.id DESC LIMIT ? OFFSET ?`, verseID, ownerID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	notes := make([]Note, 0)
	for rows.Next() {
		var note Note
		if err := rows.Scan(&note.ID, &note.OwnerUserID, &note.Title, &note.MainReference, &note.Content, &note.CreatedAt, &note.UpdatedAt); err != nil {
			return nil, err
		}
		notes = append(notes, note)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return notes, nil
}

func ListNotes(db *sql.DB, ownerID string, limit, offset int) ([]Note, error) {
	if strings.TrimSpace(ownerID) == "" {
		return []Note{}, nil
	}
	rows, err := db.Query(`SELECT id, owner_user_id, title, main_reference, content, created_at, updated_at FROM notes WHERE owner_user_id = ? ORDER BY updated_at DESC, id DESC LIMIT ? OFFSET ?`, ownerID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	notes := make([]Note, 0)
	for rows.Next() {
		var note Note
		if err := rows.Scan(&note.ID, &note.OwnerUserID, &note.Title, &note.MainReference, &note.Content, &note.CreatedAt, &note.UpdatedAt); err != nil {
			return nil, err
		}
		notes = append(notes, note)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return notes, nil
}

func GetNoteByID(db *sql.DB, ownerID string, noteID int64) (*Note, error) {
	if strings.TrimSpace(ownerID) == "" {
		return nil, nil
	}
	note := &Note{}
	err := db.QueryRow(`SELECT id, owner_user_id, title, main_reference, content, created_at, updated_at FROM notes WHERE id = ? AND owner_user_id = ?`, noteID, ownerID).Scan(
		&note.ID, &note.OwnerUserID, &note.Title, &note.MainReference, &note.Content, &note.CreatedAt, &note.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	verseIDs, err := getNoteVerseIDs(db, note.ID)
	if err != nil {
		return nil, err
	}
	note.VerseIDs = verseIDs
	return note, nil
}

func CreateNote(db *sql.DB, note *Note) (int64, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	result, err := tx.Exec(`INSERT INTO notes (owner_user_id, title, main_reference, content) VALUES (?, ?, ?, ?)`, note.OwnerUserID, note.Title, note.MainReference, note.Content)
	if err != nil {
		return 0, err
	}
	noteID, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := replaceNoteVerses(tx, noteID, note.VerseIDs); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return noteID, nil
}

func UpdateNote(db *sql.DB, ownerID string, note *Note) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	result, err := tx.Exec(`UPDATE notes SET title = ?, main_reference = ?, content = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND owner_user_id = ?`,
		note.Title, note.MainReference, note.Content, note.ID, ownerID)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	if err := replaceNoteVerses(tx, note.ID, note.VerseIDs); err != nil {
		return err
	}
	return tx.Commit()
}

func DeleteNote(db *sql.DB, ownerID string, noteID int64) error {
	if strings.TrimSpace(ownerID) == "" {
		return sql.ErrNoRows
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM note_verses WHERE note_id = ?", noteID); err != nil {
		return err
	}
	result, err := tx.Exec("DELETE FROM notes WHERE id = ? AND owner_user_id = ?", noteID, ownerID)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

func getNoteVerseIDs(db queryExecutor, noteID int64) ([]string, error) {
	rows, err := db.Query(`SELECT verse_id FROM note_verses WHERE note_id = ? ORDER BY verse_id ASC`, noteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	verseIDs := make([]string, 0)
	for rows.Next() {
		var verseID string
		if err := rows.Scan(&verseID); err != nil {
			return nil, err
		}
		verseIDs = append(verseIDs, verseID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return verseIDs, nil
}

func replaceNoteVerses(db queryExecutor, noteID int64, verseIDs []string) error {
	if _, err := db.Exec("DELETE FROM note_verses WHERE note_id = ?", noteID); err != nil {
		return err
	}
	for _, verseID := range verseIDs {
		if verseID == "" {
			continue
		}
		if _, err := db.Exec("INSERT OR IGNORE INTO note_verses (note_id, verse_id) VALUES (?, ?)", noteID, verseID); err != nil {
			return err
		}
	}
	return nil
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
