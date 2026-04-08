package storage

import (
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type SearchVerseResult struct {
	ID          string `json:"id"`
	Translation string `json:"translation"`
	Book        string `json:"book"`
	Chapter     int    `json:"chapter"`
	Verse       int    `json:"verse"`
	Text        string `json:"text"`
}

type SearchEntityResult struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Name  string `json:"name"`
	Extra string `json:"extra,omitempty"`
}

type Suggestion struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Value string `json:"value"`
}

type Book struct {
	ID        string `json:"id"`
	OsisName  string `json:"osis_name"`
	BookName  string `json:"book_name"`
	Testament string `json:"testament"`
	BookOrder int    `json:"book_order"`
	Slug      string `json:"slug"`
}

type Chapter struct {
	ID         string `json:"id"`
	BookID     string `json:"book_id"`
	OsisRef    string `json:"osis_ref"`
	ChapterNum int    `json:"chapter_num"`
}

type EventParticipants struct {
	People []Person `json:"people"`
	Groups []Group  `json:"groups"`
}

type ResolveResult struct {
	Type string      `json:"type"`
	ID   string      `json:"id"`
	Data interface{} `json:"data"`
}

type LexiconOccurrence struct {
	VerseID        string           `json:"verse_id"`
	WordOrder      int              `json:"word_order"`
	SurfaceWord    string           `json:"surface_word"`
	EnglishGloss   string           `json:"english_gloss"`
	MorphCode      string           `json:"morph_code"`
	ManuscriptType string           `json:"manuscript_type"`
	Morphology     *MorphologyEntry `json:"morphology,omitempty"`
}

type VerseRangeResult struct {
	Reference string  `json:"reference"`
	Start     string  `json:"start"`
	End       string  `json:"end"`
	Verses    []Verse `json:"verses"`
}

var verseBookNames = map[string]string{
	"GEN": "Genesis",
	"EXO": "Exodus",
	"LEV": "Leviticus",
	"NUM": "Numbers",
	"DEU": "Deuteronomy",
	"JOS": "Joshua",
	"JDG": "Judges",
	"RUT": "Ruth",
	"1SA": "1 Samuel",
	"2SA": "2 Samuel",
	"1KI": "1 Kings",
	"2KI": "2 Kings",
	"1CH": "1 Chronicles",
	"2CH": "2 Chronicles",
	"EZR": "Ezra",
	"NEH": "Nehemiah",
	"EST": "Esther",
	"JOB": "Job",
	"PSA": "Psalms",
	"PRO": "Proverbs",
	"ECC": "Ecclesiastes",
	"SNG": "Song of Solomon",
	"ISA": "Isaiah",
	"JER": "Jeremiah",
	"LAM": "Lamentations",
	"EZK": "Ezekiel",
	"DAN": "Daniel",
	"HOS": "Hosea",
	"JOL": "Joel",
	"AMO": "Amos",
	"OBA": "Obadiah",
	"JON": "Jonah",
	"MIC": "Micah",
	"NAM": "Nahum",
	"HAB": "Habakkuk",
	"ZEP": "Zephaniah",
	"HAG": "Haggai",
	"ZEC": "Zechariah",
	"MAL": "Malachi",
	"MAT": "Matthew",
	"MRK": "Mark",
	"LUK": "Luke",
	"JHN": "John",
	"ACT": "Acts",
	"ROM": "Romans",
	"1CO": "1 Corinthians",
	"2CO": "2 Corinthians",
	"GAL": "Galatians",
	"EPH": "Ephesians",
	"PHP": "Philippians",
	"COL": "Colossians",
	"1TH": "1 Thessalonians",
	"2TH": "2 Thessalonians",
	"1TI": "1 Timothy",
	"2TI": "2 Timothy",
	"TIT": "Titus",
	"PHM": "Philemon",
	"HEB": "Hebrews",
	"JAS": "James",
	"1PE": "1 Peter",
	"2PE": "2 Peter",
	"1JO": "1 John",
	"2JO": "2 John",
	"3JO": "3 John",
	"JUD": "Jude",
	"REV": "Revelation",
}

func SearchVerses(db *sql.DB, q string, limit, offset int) ([]SearchVerseResult, error) {
	rows, err := db.Query(`
		SELECT v.id, COALESCE(v.translation, ''), COALESCE(v.book, ''), COALESCE(v.chapter, 0), COALESCE(v.verse, 0), COALESCE(v.text, '')
		FROM verses_fts f
		JOIN verses v ON v.id = f.osis_id
		WHERE f.content MATCH ?
		LIMIT ? OFFSET ?`, q+"*", limit, offset)
	if err != nil {
		rows, err = db.Query(`
			SELECT id, COALESCE(translation, ''), COALESCE(book, ''), COALESCE(chapter, 0), COALESCE(verse, 0), COALESCE(text, '')
			FROM verses
			WHERE text LIKE ?
			LIMIT ? OFFSET ?`, "%"+q+"%", limit, offset)
		if err != nil {
			return nil, err
		}
	}
	defer rows.Close()

	results := make([]SearchVerseResult, 0)
	for rows.Next() {
		var item SearchVerseResult
		if err := rows.Scan(&item.ID, &item.Translation, &item.Book, &item.Chapter, &item.Verse, &item.Text); err != nil {
			return nil, err
		}
		results = append(results, item)
	}
	return results, rows.Err()
}

func SearchEntities(db *sql.DB, q string, limit, offset int) ([]SearchEntityResult, error) {
	results := make([]SearchEntityResult, 0)

	peopleRows, err := db.Query("SELECT id, name, COALESCE(lookup_name, '') FROM people WHERE name LIKE ? OR lookup_name LIKE ? LIMIT ? OFFSET ?", "%"+q+"%", "%"+q+"%", limit, offset)
	if err != nil {
		return nil, err
	}
	for peopleRows.Next() {
		var id, name, extra string
		if err := peopleRows.Scan(&id, &name, &extra); err != nil {
			peopleRows.Close()
			return nil, err
		}
		results = append(results, SearchEntityResult{Type: "person", ID: id, Name: name, Extra: extra})
	}
	if err := peopleRows.Err(); err != nil {
		peopleRows.Close()
		return nil, err
	}
	peopleRows.Close()

	locationRows, err := db.Query("SELECT id, name, COALESCE(modern_name, '') FROM locations WHERE name LIKE ? OR modern_name LIKE ? LIMIT ? OFFSET ?", "%"+q+"%", "%"+q+"%", limit, offset)
	if err != nil {
		return nil, err
	}
	for locationRows.Next() {
		var id, name, extra string
		if err := locationRows.Scan(&id, &name, &extra); err != nil {
			locationRows.Close()
			return nil, err
		}
		results = append(results, SearchEntityResult{Type: "location", ID: id, Name: name, Extra: extra})
	}
	if err := locationRows.Err(); err != nil {
		locationRows.Close()
		return nil, err
	}
	locationRows.Close()

	eventRows, err := db.Query("SELECT id, title, COALESCE(start_date, '') FROM events WHERE title LIKE ? LIMIT ? OFFSET ?", "%"+q+"%", limit, offset)
	if err != nil {
		return nil, err
	}
	for eventRows.Next() {
		var id, name, extra string
		if err := eventRows.Scan(&id, &name, &extra); err != nil {
			eventRows.Close()
			return nil, err
		}
		results = append(results, SearchEntityResult{Type: "event", ID: id, Name: name, Extra: extra})
	}
	if err := eventRows.Err(); err != nil {
		eventRows.Close()
		return nil, err
	}
	eventRows.Close()

	return results, nil
}

func Suggest(db *sql.DB, q string, limit, offset int) ([]Suggestion, error) {
	suggestions := make([]Suggestion, 0)
	seen := map[string]struct{}{}

	add := func(s Suggestion) {
		key := strings.ToLower(s.Value)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		suggestions = append(suggestions, s)
	}

	load := func(query, typ string) error {
		rows, err := db.Query(query, q+"%", limit, offset)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, value string
			if err := rows.Scan(&id, &value); err != nil {
				return err
			}
			add(Suggestion{Type: typ, ID: id, Value: value})
		}
		return rows.Err()
	}

	if err := load("SELECT id, name FROM people WHERE name LIKE ? ORDER BY name ASC LIMIT ? OFFSET ?", "person"); err != nil {
		return nil, err
	}
	if err := load("SELECT id, name FROM locations WHERE name LIKE ? ORDER BY name ASC LIMIT ? OFFSET ?", "location"); err != nil {
		return nil, err
	}
	if err := load("SELECT strongs_id, word FROM lexicon WHERE word LIKE ? ORDER BY word ASC LIMIT ? OFFSET ?", "lexicon"); err != nil {
		return nil, err
	}
	if err := load("SELECT id, title FROM events WHERE title LIKE ? ORDER BY title ASC LIMIT ? OFFSET ?", "event"); err != nil {
		return nil, err
	}

	if len(suggestions) > limit {
		suggestions = suggestions[:limit]
	}
	return suggestions, nil
}

func GetPersonVerses(db *sql.DB, personID string, limit, offset int) ([]Verse, error) {
	rows, err := db.Query("SELECT verse_id FROM person_verses WHERE person_id = ? ORDER BY verse_id ASC LIMIT ? OFFSET ?", personID, limit, offset)
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

	return resolveVerseRefs(db, refs)
}

func GetLocationVerses(db *sql.DB, locationID string, limit, offset int) ([]Verse, error) {
	canonicalID, err := resolveCanonicalLocationID(db, locationID)
	if err != nil {
		return nil, err
	}

	rows, err := db.Query("SELECT verse_id FROM verse_locations WHERE location_id = ? ORDER BY verse_id ASC LIMIT ? OFFSET ?", canonicalID, limit, offset)
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

	return resolveVerseRefs(db, refs)
}

func GetGroupMembers(db *sql.DB, groupID string, limit, offset int) ([]Person, error) {
	rows, err := db.Query(`
		SELECT p.id, p.name, p.lookup_name, p.gender, p.birth_year, p.death_year, p.dictionary_text, p.slug
		FROM people p
		INNER JOIN group_memberships gm ON gm.person_id = p.id
		WHERE gm.group_id = ?
		ORDER BY p.name ASC LIMIT ? OFFSET ?`, groupID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	members := make([]Person, 0)
	for rows.Next() {
		var person Person
		if err := rows.Scan(&person.ID, &person.Name, &person.LookupName, &person.Gender, &person.BirthYear, &person.DeathYear, &person.DictionaryText, &person.Slug); err != nil {
			return nil, err
		}
		members = append(members, person)
	}
	return members, rows.Err()
}

func GetEventParticipants(db *sql.DB, eventID string, limit, offset int) (*EventParticipants, error) {
	participants := &EventParticipants{People: []Person{}, Groups: []Group{}}

	rows, err := db.Query("SELECT participant_id FROM event_participants WHERE event_id = ? ORDER BY participant_id ASC LIMIT ? OFFSET ?", eventID, limit, offset)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var participantID string
			if err := rows.Scan(&participantID); err != nil {
				return nil, err
			}
			if person, _ := GetPersonByID(db, participantID); person != nil {
				participants.People = append(participants.People, *person)
				continue
			}
			if group, _ := GetGroupByID(db, participantID); group != nil {
				participants.Groups = append(participants.Groups, *group)
			}
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	if len(participants.People) > 0 || len(participants.Groups) > 0 {
		return participants, nil
	}

	verseRows, err := db.Query("SELECT verse_id FROM event_verses WHERE event_id = ?", eventID)
	if err != nil {
		return participants, nil
	}
	defer verseRows.Close()

	refSet := map[string]struct{}{}
	for verseRows.Next() {
		var ref string
		if err := verseRows.Scan(&ref); err != nil {
			return nil, err
		}
		refSet[ref] = struct{}{}
	}

	personSet := map[string]Person{}
	groupSet := map[string]Group{}
	for ref := range refSet {
		keys, err := getVerseLookupKeys(db, ref)
		if err != nil {
			continue
		}
		for _, key := range keys {
			rows, err := db.Query(`
				SELECT p.id, p.name, p.lookup_name, p.gender, p.birth_year, p.death_year, p.dictionary_text, p.slug
				FROM people p
				INNER JOIN person_verses pv ON pv.person_id = p.id
				WHERE pv.verse_id = ?`, key)
			if err != nil {
				continue
			}
			for rows.Next() {
				var person Person
				if err := rows.Scan(&person.ID, &person.Name, &person.LookupName, &person.Gender, &person.BirthYear, &person.DeathYear, &person.DictionaryText, &person.Slug); err == nil {
					personSet[person.ID] = person
				}
			}
			rows.Close()
		}
	}

	for _, person := range personSet {
		participants.People = append(participants.People, person)
		memberGroups, err := db.Query(`
			SELECT g.id, g.name
			FROM groups g
			INNER JOIN group_memberships gm ON gm.group_id = g.id
			WHERE gm.person_id = ?`, person.ID)
		if err != nil {
			continue
		}
		for memberGroups.Next() {
			var group Group
			if err := memberGroups.Scan(&group.ID, &group.Name); err == nil {
				groupSet[group.ID] = group
			}
		}
		memberGroups.Close()
	}

	for _, group := range groupSet {
		participants.Groups = append(participants.Groups, group)
	}

	return participants, nil
}

func ListBooks(db *sql.DB, limit, offset int) ([]Book, error) {
	rows, err := db.Query("SELECT id, osis_name, book_name, testament, book_order, slug FROM books ORDER BY book_order ASC LIMIT ? OFFSET ?", limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	books := make([]Book, 0)
	for rows.Next() {
		var book Book
		if err := rows.Scan(&book.ID, &book.OsisName, &book.BookName, &book.Testament, &book.BookOrder, &book.Slug); err != nil {
			return nil, err
		}
		books = append(books, book)
	}
	return books, rows.Err()
}

func GetBookBySlug(db *sql.DB, slug string) (*Book, error) {
	book := &Book{}
	err := db.QueryRow("SELECT id, osis_name, book_name, testament, book_order, slug FROM books WHERE slug = ?", slug).Scan(
		&book.ID, &book.OsisName, &book.BookName, &book.Testament, &book.BookOrder, &book.Slug)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return book, nil
}

func ListBookChaptersBySlug(db *sql.DB, slug string) (*Book, []Chapter, error) {
	book, err := GetBookBySlug(db, slug)
	if err != nil || book == nil {
		return book, nil, err
	}

	rows, err := db.Query("SELECT id, book_id, osis_ref, chapter_num FROM chapters WHERE book_id = ? ORDER BY chapter_num ASC", book.ID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	chapters := make([]Chapter, 0)
	for rows.Next() {
		var chapter Chapter
		if err := rows.Scan(&chapter.ID, &chapter.BookID, &chapter.OsisRef, &chapter.ChapterNum); err != nil {
			return nil, nil, err
		}
		chapters = append(chapters, chapter)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	return book, chapters, nil
}

func ListTimeline(db *sql.DB, limit, offset int) ([]Event, error) {
	rows, err := db.Query("SELECT id, title, start_date, duration, sort_key FROM events ORDER BY sort_key ASC, title ASC LIMIT ? OFFSET ?", limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	timeline := make([]Event, 0)
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.ID, &event.Title, &event.StartDate, &event.Duration, &event.SortKey); err != nil {
			return nil, err
		}
		timeline = append(timeline, event)
	}
	return timeline, rows.Err()
}

func GetAnalysisByVerseID(db *sql.DB, verseID string) ([]VerseAnalysisToken, error) {
	return GetVerseAnalysisByVerseID(db, verseID)
}

func GetLexiconOccurrencesByID(db *sql.DB, strongsID string, limit, offset int) ([]LexiconOccurrence, error) {
	rows, err := db.Query(`
		SELECT
			va.verse_id,
			va.word_order,
			va.surface_word,
			va.english_gloss,
			COALESCE(va.morph_code, ''),
			COALESCE(va.manuscript_type, ''),
			COALESCE(m.code, ''),
			COALESCE(m.short_def, ''),
			COALESCE(m.long_exp, '')
		FROM verse_analysis va
		LEFT JOIN morphology m ON m.code = va.morph_code
		WHERE va.strongs_id = ?
		ORDER BY va.verse_id ASC, va.word_order ASC
		LIMIT ? OFFSET ?`, strongsID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	occurrences := make([]LexiconOccurrence, 0)
	for rows.Next() {
		var occurrence LexiconOccurrence
		var morphID, morphShort, morphLong string
		if err := rows.Scan(
			&occurrence.VerseID,
			&occurrence.WordOrder,
			&occurrence.SurfaceWord,
			&occurrence.EnglishGloss,
			&occurrence.MorphCode,
			&occurrence.ManuscriptType,
			&morphID,
			&morphShort,
			&morphLong,
		); err != nil {
			return nil, err
		}

		if morphID != "" {
			occurrence.Morphology = &MorphologyEntry{
				Code:     morphID,
				ShortDef: morphShort,
				LongExp:  morphLong,
			}
		}
		occurrences = append(occurrences, occurrence)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return occurrences, nil
}

func ResolveRecID(db *sql.DB, recID string) (*ResolveResult, error) {
	if person, err := GetPersonByID(db, recID); err == nil && person != nil {
		return &ResolveResult{Type: "person", ID: recID, Data: person}, nil
	}
	if location, err := GetLocationByID(db, recID); err == nil && location != nil {
		return &ResolveResult{Type: "location", ID: recID, Data: location}, nil
	}
	if event, err := GetEventByID(db, recID); err == nil && event != nil {
		return &ResolveResult{Type: "event", ID: recID, Data: event}, nil
	}
	if group, err := GetGroupByID(db, recID); err == nil && group != nil {
		return &ResolveResult{Type: "group", ID: recID, Data: group}, nil
	}

	var osisRef sql.NullString
	if err := db.QueryRow("SELECT osis_ref FROM verse_id_map WHERE rec_id = ?", recID).Scan(&osisRef); err == nil && osisRef.Valid {
		if verse, err := getVerseByAnyID(db, osisRef.String); err == nil && verse != nil {
			return &ResolveResult{Type: "verse", ID: recID, Data: verse}, nil
		}
		return &ResolveResult{Type: "verse_ref", ID: recID, Data: map[string]string{"osis_ref": osisRef.String}}, nil
	}

	return nil, nil
}

func resolveVerseRefs(db *sql.DB, refs []string) ([]Verse, error) {
	verseMap := map[string]Verse{}
	for _, ref := range refs {
		verse, err := getVerseByAnyID(db, ref)
		if err != nil {
			return nil, err
		}
		if verse == nil {
			continue
		}
		verseMap[verse.ID] = *verse
	}

	verses := make([]Verse, 0, len(verseMap))
	for _, verse := range verseMap {
		verses = append(verses, verse)
	}
	return verses, nil
}

func ExpandVerseReferences(db *sql.DB, references []string) ([]string, []string, error) {
	resolved := make([]string, 0)
	unresolved := make([]string, 0)
	seen := map[string]struct{}{}

	for _, reference := range references {
		reference = strings.TrimSpace(reference)
		if reference == "" {
			continue
		}

		result, err := GetVerseRangeByReference(db, reference)
		if err != nil {
			return nil, nil, err
		}
		if result == nil || len(result.Verses) == 0 {
			unresolved = append(unresolved, reference)
			continue
		}

		for _, verse := range result.Verses {
			id := strings.ToUpper(strings.TrimSpace(verse.ID))
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			resolved = append(resolved, id)
		}
	}

	return resolved, unresolved, nil
}

func GetVerseRangeByReference(db *sql.DB, reference string) (*VerseRangeResult, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return nil, nil
	}

	if strings.Count(reference, "-") == 0 {
		verse, err := getVerseByAnyID(db, reference)
		if err != nil {
			return nil, err
		}
		if verse == nil {
			if book, chapter, verseNum, ok := parseVerseToken(reference, "", 0); ok {
				verse, err = getVerseByBookChapterVerse(db, book, chapter, verseNum)
				if err != nil {
					return nil, err
				}
			}
		}
		if verse == nil {
			return nil, nil
		}
		return &VerseRangeResult{
			Reference: reference,
			Start:     verse.ID,
			End:       verse.ID,
			Verses:    []Verse{*verse},
		}, nil
	}

	startRaw, endRaw, found := strings.Cut(reference, "-")
	if !found {
		return nil, fmt.Errorf("invalid verse reference")
	}

	startVerse, err := resolveReferenceToken(db, startRaw, "", 0)
	if err != nil {
		return nil, err
	}
	if startVerse == nil {
		return nil, nil
	}

	endVerse, err := resolveReferenceToken(db, endRaw, startVerse.Book, startVerse.Chapter)
	if err != nil {
		return nil, err
	}
	if endVerse == nil {
		return nil, nil
	}

	if startVerse.Book != endVerse.Book {
		return nil, fmt.Errorf("verse ranges must stay within one book")
	}
	if endVerse.Chapter < startVerse.Chapter || (endVerse.Chapter == startVerse.Chapter && endVerse.Verse < startVerse.Verse) {
		return nil, fmt.Errorf("invalid verse range")
	}

	rows, err := db.Query(`
		SELECT id, translation, book, chapter, verse, text
		FROM verses
		WHERE book = ?
		AND (
			chapter > ? OR (chapter = ? AND verse >= ?)
		)
		AND (
			chapter < ? OR (chapter = ? AND verse <= ?)
		)
		ORDER BY chapter ASC, verse ASC`, startVerse.Book, startVerse.Chapter, startVerse.Chapter, startVerse.Verse, endVerse.Chapter, endVerse.Chapter, endVerse.Verse)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	verses := make([]Verse, 0)
	for rows.Next() {
		var verse Verse
		if err := rows.Scan(&verse.ID, &verse.Translation, &verse.Book, &verse.Chapter, &verse.Verse, &verse.Text); err != nil {
			return nil, err
		}
		verses = append(verses, verse)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(verses) == 0 {
		return nil, nil
	}

	return &VerseRangeResult{
		Reference: reference,
		Start:     startVerse.ID,
		End:       endVerse.ID,
		Verses:    verses,
	}, nil
}

func getVerseByAnyID(db *sql.DB, rawID string) (*Verse, error) {
	if rawID == "" {
		return nil, nil
	}

	if verse, err := GetVerseByID(db, strings.ToUpper(rawID)); err != nil {
		return nil, err
	} else if verse != nil {
		return verse, nil
	}

	if strings.HasPrefix(rawID, "rec") {
		var osisRef sql.NullString
		if err := db.QueryRow("SELECT osis_ref FROM verse_id_map WHERE rec_id = ?", rawID).Scan(&osisRef); err == nil && osisRef.Valid {
			if verse, err := getVerseByAnyID(db, osisRef.String); err != nil {
				return nil, err
			} else if verse != nil {
				return verse, nil
			}
		}
	}

	if normalized, ok := normalizeToBSBVerseID(rawID); ok {
		if verse, err := GetVerseByID(db, normalized); err != nil {
			return nil, err
		} else if verse != nil {
			return verse, nil
		}
	}

	return nil, nil
}

func resolveReferenceToken(db *sql.DB, token string, defaultBook string, defaultChapter int) (*Verse, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, nil
	}

	if verse, err := getVerseByAnyID(db, token); err != nil {
		return nil, err
	} else if verse != nil {
		return verse, nil
	}

	book, chapter, verseNum, ok := parseVerseToken(token, defaultBook, defaultChapter)
	if !ok {
		return nil, nil
	}

	return getVerseByBookChapterVerse(db, book, chapter, verseNum)
}

func getVerseByBookChapterVerse(db *sql.DB, book string, chapter, verseNum int) (*Verse, error) {
	if book == "" || chapter <= 0 || verseNum <= 0 {
		return nil, nil
	}

	row := db.QueryRow(`
		SELECT id, translation, book, chapter, verse, text
		FROM verses
		WHERE book = ? AND chapter = ? AND verse = ?
		LIMIT 1`, book, chapter, verseNum)

	verse := &Verse{}
	var id, translation, rowBook, text sql.NullString
	var rowChapter, rowVerse sql.NullInt64
	if err := row.Scan(&id, &translation, &rowBook, &rowChapter, &rowVerse, &text); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	if id.Valid {
		verse.ID = id.String
	}
	if translation.Valid {
		verse.Translation = translation.String
	}
	if rowBook.Valid {
		verse.Book = rowBook.String
	}
	if rowChapter.Valid {
		verse.Chapter = int(rowChapter.Int64)
	}
	if rowVerse.Valid {
		verse.Verse = int(rowVerse.Int64)
	}
	if text.Valid {
		verse.Text = text.String
	}

	return verse, nil
}

var humanVersePattern = regexp.MustCompile(`(?i)^(.+?)\s+(\d+):(\d+)$`)

func parseVerseToken(token string, defaultBook string, defaultChapter int) (string, int, int, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", 0, 0, false
	}

	if verse := parseDotVerseToken(token); verse != nil {
		return verse.book, verse.chapter, verse.verse, true
	}

	if match := humanVersePattern.FindStringSubmatch(token); len(match) == 4 {
		book := resolveBookName(match[1])
		if book == "" {
			return "", 0, 0, false
		}
		chapter, err1 := strconv.Atoi(match[2])
		verseNum, err2 := strconv.Atoi(match[3])
		if err1 != nil || err2 != nil {
			return "", 0, 0, false
		}
		return book, chapter, verseNum, true
	}

	if defaultBook != "" && defaultChapter > 0 {
		if strings.Contains(token, ":") {
			parts := strings.Split(token, ":")
			if len(parts) == 2 {
				chapter, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
				verseNum, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
				if err1 == nil && err2 == nil {
					return defaultBook, chapter, verseNum, true
				}
			}
		}
		if verseNum, err := strconv.Atoi(token); err == nil {
			return defaultBook, defaultChapter, verseNum, true
		}
	}

	return "", 0, 0, false
}

type dotVerseToken struct {
	book    string
	chapter int
	verse   int
}

func parseDotVerseToken(token string) *dotVerseToken {
	parts := strings.Split(token, ".")
	if len(parts) == 3 {
		chapter, err1 := strconv.Atoi(strings.TrimSpace(parts[1]))
		verseNum, err2 := strconv.Atoi(strings.TrimSpace(parts[2]))
		book := resolveBookName(parts[0])
		if err1 == nil && err2 == nil && book != "" {
			return &dotVerseToken{book: book, chapter: chapter, verse: verseNum}
		}
	}
	if len(parts) == 4 {
		chapter, err1 := strconv.Atoi(strings.TrimSpace(parts[2]))
		verseNum, err2 := strconv.Atoi(strings.TrimSpace(parts[3]))
		book := resolveBookName(parts[1])
		if err1 == nil && err2 == nil && book != "" {
			return &dotVerseToken{book: book, chapter: chapter, verse: verseNum}
		}
	}
	return nil
}

func resolveBookName(token string) string {
	token = strings.ToUpper(strings.TrimSpace(token))
	if token == "" {
		return ""
	}

	compact := strings.ReplaceAll(token, " ", "")
	alias := map[string]string{
		"PSALM":         "PSA",
		"PSALMS":        "PSA",
		"SONGOFSOLOMON": "SNG",
		"SONGOFSONGS":   "SNG",
	}
	if mapped, ok := alias[compact]; ok {
		if fullName, ok := verseBookNames[mapped]; ok {
			return fullName
		}
		if name, ok := bsbCodeToTheoBook[mapped]; ok {
			return name
		}
	}

	if name, ok := bsbCodeToTheoBook[token]; ok {
		if fullName, ok := verseBookNames[token]; ok {
			return fullName
		}
		return name
	}

	reverse := map[string]string{}
	for code, name := range bsbCodeToTheoBook {
		reverse[strings.ToUpper(name)] = code
	}
	for code, full := range verseBookNames {
		reverse[strings.ToUpper(full)] = code
		reverse[strings.ReplaceAll(strings.ToUpper(full), " ", "")] = code
	}
	if code, ok := reverse[token]; ok {
		if fullName, ok := verseBookNames[code]; ok {
			return fullName
		}
		if name, ok := bsbCodeToTheoBook[code]; ok {
			return name
		}
	}
	if code, ok := reverse[compact]; ok {
		if fullName, ok := verseBookNames[code]; ok {
			return fullName
		}
		if name, ok := bsbCodeToTheoBook[code]; ok {
			return name
		}
	}

	return ""
}

func normalizeToBSBVerseID(input string) (string, bool) {
	parts := strings.Split(input, ".")
	if len(parts) != 3 {
		return "", false
	}

	bookToken := strings.ToUpper(strings.TrimSpace(parts[0]))
	chapter := strings.TrimSpace(parts[1])
	verse := strings.TrimSpace(parts[2])
	if chapter == "" || verse == "" {
		return "", false
	}

	alias := map[string]string{
		"1KGS": "1KI", "2KGS": "2KI", "1CHRON": "1CH", "2CHRON": "2CH", "PS": "PSA", "MATT": "MAT",
	}
	if mapped, ok := alias[bookToken]; ok {
		bookToken = mapped
	}

	if _, ok := bsbCodeToTheoBook[bookToken]; ok {
		return fmt.Sprintf("%s.%s.%s", bookToken, chapter, verse), true
	}

	reverse := map[string]string{}
	for bsb, theo := range bsbCodeToTheoBook {
		reverse[strings.ToUpper(theo)] = bsb
	}

	if mapped, ok := reverse[bookToken]; ok {
		return fmt.Sprintf("%s.%s.%s", mapped, chapter, verse), true
	}

	return "", false
}
