package main

import (
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// A brief is everything the corpus already knows about one passage, flattened
// into the handful of facts worth putting in front of a model: what happened,
// when, who was there, where, and which verses it covers.
//
// Briefs are built from the seeded corpus rather than from the Airtable export
// under data/history, because the seeder has already resolved the record-id
// soup — participants to names, locations to place names, verse records to
// OSIS refs — and doing it twice would mean two versions of that logic drifting
// apart.
type brief struct {
	// Scope is "event" or "book": what the resulting articles get attached to.
	Scope string
	// TargetID is the events.id or books.id the articles hang off.
	TargetID string
	Title    string
	// Reference reads like "2 Kings 18:13-19:37", for the model and for logs.
	Reference string
	// Year is the traditional-chronology year the corpus assigns, if any.
	Year   string
	People []string
	Places []string
	Notes  string
}

// label is what shows in progress output.
func (b brief) label() string {
	if b.Reference == "" {
		return b.Title
	}
	return fmt.Sprintf("%s (%s)", b.Title, b.Reference)
}

// loadEventBriefs returns one brief per Theographic event, in timeline order.
//
// Events are the unit that makes this verse-specific: the corpus already links
// each one to the verses it covers, so an article attached to an event reaches
// exactly those verses without inventing a new mapping.
func loadEventBriefs(db *sql.DB) ([]brief, error) {
	rows, err := db.Query(`
		SELECT e.id, e.title, COALESCE(e.start_date, ''), COALESCE(e.notes, ''),
		       COALESCE((SELECT group_concat(p.name, '|')
		                 FROM event_participants ep
		                 JOIN people p ON p.id = ep.participant_id
		                 WHERE ep.event_id = e.id), ''),
		       COALESCE((SELECT group_concat(l.name, '|')
		                 FROM event_locations el
		                 JOIN locations l ON l.id = el.location_id
		                 WHERE el.event_id = e.id), '')
		FROM events e
		ORDER BY e.sort_key ASC`)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	var briefs []brief

	for rows.Next() {
		var b brief
		var people, places string

		if err := rows.Scan(&b.TargetID, &b.Title, &b.Year, &b.Notes, &people, &places); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}

		b.Scope = "event"
		b.People = splitList(people)
		b.Places = splitList(places)
		briefs = append(briefs, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	refs, err := loadEventReferences(db)
	if err != nil {
		return nil, err
	}

	for i := range briefs {
		briefs[i].Reference = refs[briefs[i].TargetID]
	}

	return briefs, nil
}

// loadBookBriefs returns one brief per book: the coarse tier, for the eras and
// epistles where no dated event carries the passage.
func loadBookBriefs(db *sql.DB) ([]brief, error) {
	rows, err := db.Query(`
		SELECT b.id, b.book_name, COALESCE(b.testament, ''), COALESCE(b.division, ''),
		       COALESCE(b.year_written, ''),
		       COALESCE((SELECT group_concat(p.name, '|')
		                 FROM book_writers bw
		                 JOIN people p ON p.id = bw.person_id
		                 WHERE bw.book_id = b.id), ''),
		       COALESCE((SELECT l.name FROM locations l WHERE l.id = b.place_written_id), '')
		FROM books b
		ORDER BY b.book_order ASC`)
	if err != nil {
		return nil, fmt.Errorf("query books: %w", err)
	}
	defer rows.Close()

	var briefs []brief

	for rows.Next() {
		var b brief
		var testament, division, writers, place string

		if err := rows.Scan(&b.TargetID, &b.Title, &testament, &division, &b.Year, &writers, &place); err != nil {
			return nil, fmt.Errorf("scan book: %w", err)
		}

		b.Scope = "book"
		b.Reference = b.Title
		b.People = splitList(writers)
		b.Notes = strings.TrimSpace(strings.Join([]string{division, testament}, ", "))

		if place != "" {
			b.Places = []string{place}
		}

		briefs = append(briefs, b)
	}

	return briefs, rows.Err()
}

// loadEventReferences builds a human-readable reference range per event.
//
// The range is computed from verse_id_map's OSIS refs rather than from the
// verses table, because the two use different book codes: verse_id_map carries
// OSIS names ("2Kgs.23.35") while verses.id carries a three-letter code
// ("2KI.23.35"). They coincide for Genesis and diverge for most of the rest, so
// joining on the string silently produced a reference for a handful of books
// and nothing for the others. books.osis_name is the column that bridges them.
//
// Ordering has to be numeric for the same reason: sorting OSIS refs as strings
// puts chapter 2 verse 10 before verse 4, which would report "Gen 1:1-2:31" for
// a passage ending at 2:3.
func loadEventReferences(db *sql.DB) (map[string]string, error) {
	books, err := loadBookNames(db)
	if err != nil {
		return nil, err
	}

	rows, err := db.Query(`
		SELECT ev.event_id, m.osis_ref
		FROM event_verses ev
		JOIN verse_id_map m ON m.rec_id = ev.verse_id`)
	if err != nil {
		return nil, fmt.Errorf("query event verses: %w", err)
	}
	defer rows.Close()

	type point struct {
		osisName string
		order    int
		chapter  int
		verse    int
	}

	byEvent := map[string][]point{}

	for rows.Next() {
		var eventID, osisRef string

		if err := rows.Scan(&eventID, &osisRef); err != nil {
			return nil, fmt.Errorf("scan event verse: %w", err)
		}

		parts := strings.Split(osisRef, ".")
		if len(parts) != 3 {
			continue
		}

		chapter, err1 := strconv.Atoi(parts[1])
		verse, err2 := strconv.Atoi(parts[2])
		if err1 != nil || err2 != nil {
			continue
		}

		book, ok := books[parts[0]]
		if !ok {
			continue
		}

		byEvent[eventID] = append(byEvent[eventID], point{
			osisName: parts[0], order: book.order, chapter: chapter, verse: verse,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	refs := make(map[string]string, len(byEvent))

	for eventID, points := range byEvent {
		sort.Slice(points, func(i, j int) bool {
			if points[i].order != points[j].order {
				return points[i].order < points[j].order
			}
			if points[i].chapter != points[j].chapter {
				return points[i].chapter < points[j].chapter
			}
			return points[i].verse < points[j].verse
		})

		first, last := points[0], points[len(points)-1]
		firstName := books[first.osisName].name
		lastName := books[last.osisName].name

		switch {
		case first.osisName != last.osisName:
			refs[eventID] = fmt.Sprintf("%s %d:%d - %s %d:%d",
				firstName, first.chapter, first.verse, lastName, last.chapter, last.verse)
		case first.chapter != last.chapter:
			refs[eventID] = fmt.Sprintf("%s %d:%d-%d:%d",
				firstName, first.chapter, first.verse, last.chapter, last.verse)
		case first.verse != last.verse:
			refs[eventID] = fmt.Sprintf("%s %d:%d-%d",
				firstName, first.chapter, first.verse, last.verse)
		default:
			refs[eventID] = fmt.Sprintf("%s %d:%d", firstName, first.chapter, first.verse)
		}
	}

	return refs, nil
}

type bookName struct {
	name  string
	order int
}

func loadBookNames(db *sql.DB) (map[string]bookName, error) {
	rows, err := db.Query(`SELECT osis_name, book_name, book_order FROM books WHERE osis_name IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("query books: %w", err)
	}
	defer rows.Close()

	books := map[string]bookName{}

	for rows.Next() {
		var osisName, name string
		var order int

		if err := rows.Scan(&osisName, &name, &order); err != nil {
			return nil, fmt.Errorf("scan book: %w", err)
		}

		books[osisName] = bookName{name: name, order: order}
	}

	return books, rows.Err()
}

func splitList(joined string) []string {
	if joined == "" {
		return nil
	}

	seen := map[string]bool{}
	var out []string

	for _, part := range strings.Split(joined, "|") {
		part = strings.TrimSpace(part)
		if part == "" || seen[part] {
			continue
		}

		seen[part] = true
		out = append(out, part)
	}

	return out
}

// formatYear turns the corpus's bare signed years ("-701") into "701 BC", which
// is what a model and a reader both expect to see.
func formatYear(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	year, err := strconv.Atoi(raw)
	if err != nil {
		return raw
	}

	if year < 0 {
		return fmt.Sprintf("%d BC", -year)
	}

	return fmt.Sprintf("AD %d", year)
}
