package storage

import (
	"database/sql"
	"fmt"
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
	rows, err := db.Query("SELECT verse_id FROM verse_locations WHERE location_id = ? ORDER BY verse_id ASC LIMIT ? OFFSET ?", locationID, limit, offset)
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
