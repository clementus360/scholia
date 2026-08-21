package storage

import (
	"path/filepath"
	"testing"
)

// entityFixture builds a corpus where one verse names a person and a place, and
// both have been resolved.
//
// The place carries the case this feature most needs to get right: it resolves
// to the site itself *and* to a modern village 1.8km away. Those are different
// claims, and the fixture keeps both so a change that flattens them fails here.
func entityFixture(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "bible.db")

	db, err := OpenBibleDBForSeed(path)
	if err != nil {
		t.Fatalf("OpenBibleDBForSeed: %v", err)
	}
	defer db.Close()

	if err := CreateBibleTables(db); err != nil {
		t.Fatalf("CreateBibleTables: %v", err)
	}

	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}

	exec(`INSERT INTO books (id, osis_name, book_name, testament, book_order, slug)
	      VALUES ('bkgen', 'Gen', 'Genesis', 'Old', 1, 'genesis')`)
	exec(`INSERT INTO verses (id, translation, book, chapter, verse, text)
	      VALUES ('GEN.14.18', 'BSB', 'Genesis', 14, 18, 'text')`)
	exec(`INSERT INTO verses (id, translation, book, chapter, verse, text)
	      VALUES ('GEN.1.1', 'BSB', 'Genesis', 1, 1, 'text')`)

	// person_verses is keyed by record id, reached through verse_id_map.
	exec(`INSERT INTO verse_id_map (rec_id, osis_ref) VALUES ('recMelch', 'Gen.14.18')`)

	exec(`INSERT INTO people (id, name) VALUES ('pMelch', 'Melchizedek')`)
	exec(`INSERT INTO person_verses (person_id, verse_id) VALUES ('pMelch', 'recMelch')`)

	// verse_locations is keyed by the BSB code directly, unlike person_verses.
	exec(`INSERT INTO locations (id, name) VALUES ('lSalem', 'Salem')`)
	exec(`INSERT INTO verse_locations (verse_id, location_id) VALUES ('GEN.14.18', 'lSalem')`)

	articles := []struct{ id, title, extract string }{
		{"q219395", "Melchizedek", "Melchizedek was a biblical figure."},
		{"q1218", "Salem", "Salem is an ancient city."},
		{"q999", "Modern Village", "A village nearby."},
	}
	for _, a := range articles {
		exec(`INSERT INTO external_articles (id, wikidata_id, title, extract, url, revision, retrieved, license)
		      VALUES (?, ?, ?, ?, 'https://en.wikipedia.org/wiki/X', 42, '2026-01-01', 'cc-by-sa-4')`,
			a.id, "Q"+a.id[1:], a.title, a.extract)
	}

	exec(`INSERT INTO entity_article_links (entity_kind, entity_id, article_id, relation, confidence, method, note)
	      VALUES ('person', 'pMelch', 'q219395', 'primary', 59, 'genealogy', 'Matched on family.')`)
	exec(`INSERT INTO entity_article_links (entity_kind, entity_id, article_id, relation, confidence, method, note)
	      VALUES ('place', 'lSalem', 'q1218', 'primary', 12, 'coordinate+name', 'The corpus places Salem here.')`)
	exec(`INSERT INTO entity_article_links (entity_kind, entity_id, article_id, relation, confidence, method, note)
	      VALUES ('place', 'lSalem', 'q999', 'nearby', 4, 'coordinate', 'About 1.8 km away.')`)

	exec(`INSERT INTO article_neighbours (article_id, target_id, label, relation, rank)
	      VALUES ('q219395', 'Q9165', 'Abraham', 'related', 0)`)
	exec(`INSERT INTO article_neighbours (article_id, target_id, label, relation, rank)
	      VALUES ('q999', 'Q1', 'Should Not Appear', 'part of', 0)`)

	return path
}

func TestEntitySuggestionsGroupByEntity(t *testing.T) {
	db, err := OpenBibleDB(entityFixture(t))
	if err != nil {
		t.Fatalf("OpenBibleDB: %v", err)
	}
	defer db.Close()

	got, err := GetEntitySuggestions(db, "GEN.14.18")
	if err != nil {
		t.Fatalf("GetEntitySuggestions: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d suggestions, want 2 (one person, one place)", len(got))
	}

	person, place := got[0], got[1]

	if person.Kind != "person" || person.Name != "Melchizedek" {
		t.Errorf("first suggestion = %s %q, want person Melchizedek", person.Kind, person.Name)
	}
	if place.Kind != "place" || place.Name != "Salem" {
		t.Errorf("second suggestion = %s %q, want place Salem", place.Kind, place.Name)
	}

	// The place's own article must lead; the village 1.8km away must not be
	// mistaken for it.
	if len(place.Articles) != 2 {
		t.Fatalf("place has %d articles, want 2", len(place.Articles))
	}
	if place.Articles[0].Relation != "primary" || place.Articles[0].Title != "Salem" {
		t.Errorf("place leads with %q (%s), want Salem (primary)",
			place.Articles[0].Title, place.Articles[0].Relation)
	}
	if place.Articles[1].Relation != "nearby" {
		t.Errorf("second place article is %q, want nearby", place.Articles[1].Relation)
	}
}

// Neighbours hang off the primary only: walking out of a merely-nearby article
// leads away from the passage rather than deeper into it.
func TestEntitySuggestionsWalkOnlyFromPrimary(t *testing.T) {
	db, err := OpenBibleDB(entityFixture(t))
	if err != nil {
		t.Fatalf("OpenBibleDB: %v", err)
	}
	defer db.Close()

	got, err := GetEntitySuggestions(db, "GEN.14.18")
	if err != nil {
		t.Fatalf("GetEntitySuggestions: %v", err)
	}

	for _, suggestion := range got {
		for _, neighbour := range suggestion.Neighbours {
			if neighbour.Label == "Should Not Appear" {
				t.Errorf("%s carried a neighbour of a nearby article", suggestion.Name)
			}
		}
	}

	if len(got[0].Neighbours) != 1 || got[0].Neighbours[0].Label != "Abraham" {
		t.Errorf("Melchizedek neighbours = %+v, want one Abraham", got[0].Neighbours)
	}
}

// A verse that names nothing resolved returns nothing, rather than the whole
// book's worth of suggestions.
func TestEntitySuggestionsEmptyForUnnamedVerse(t *testing.T) {
	db, err := OpenBibleDB(entityFixture(t))
	if err != nil {
		t.Fatalf("OpenBibleDB: %v", err)
	}
	defer db.Close()

	got, err := GetEntitySuggestions(db, "GEN.1.1")
	if err != nil {
		t.Fatalf("GetEntitySuggestions: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d suggestions for a verse naming nothing, want 0", len(got))
	}
}
