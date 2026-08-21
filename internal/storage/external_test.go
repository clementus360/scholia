package storage

import (
	"path/filepath"
	"testing"
)

// externalFixture builds a corpus holding one book, a handful of verses, one
// event covering some of them, and two articles linked to that event.
//
// The point of the fixture is the id mismatch it reproduces: verses are keyed
// by BSB code ("2KI.23.31") while event_verses is keyed by Theographic record
// id, reached through verse_id_map's OSIS names ("2Kgs.23.31"). A join that
// forgets that bridge silently returns nothing for every book except the
// handful whose two codes coincide, which is exactly the bug this guards.
func externalFixture(t *testing.T) string {
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
	      VALUES ('bk2kings', '2Kgs', '2 Kings', 'Old', 12, '2-kings')`)
	exec(`INSERT INTO books (id, osis_name, book_name, testament, book_order, slug)
	      VALUES ('bkgen', 'Gen', 'Genesis', 'Old', 1, 'genesis')`)

	verses := []struct {
		id, book       string
		chapter, verse int
	}{
		{"2KI.23.31", "2 Kings", 23, 31},
		{"2KI.23.35", "2 Kings", 23, 35},
		{"2KI.24.1", "2 Kings", 24, 1},
		{"GEN.1.1", "Genesis", 1, 1},
	}

	for _, v := range verses {
		exec(`INSERT INTO verses (id, translation, book, chapter, verse, text)
		      VALUES (?, 'BSB', ?, ?, ?, 'text')`, v.id, v.book, v.chapter, v.verse)
	}

	// Only the two verses the event covers are mapped, so a verse in the same
	// book but outside the episode can be told apart from one inside it.
	exec(`INSERT INTO verse_id_map (rec_id, osis_ref) VALUES ('recV1', '2Kgs.23.31')`)
	exec(`INSERT INTO verse_id_map (rec_id, osis_ref) VALUES ('recV2', '2Kgs.23.35')`)
	exec(`INSERT INTO verse_id_map (rec_id, osis_ref) VALUES ('recV3', '2Kgs.24.1')`)

	exec(`INSERT INTO events (id, title, start_date, sort_key) VALUES ('recE1', 'Reign of Jehoahaz', '-608', 1)`)
	exec(`INSERT INTO event_verses (event_id, verse_id) VALUES ('recE1', 'recV1')`)
	exec(`INSERT INTO event_verses (event_id, verse_id) VALUES ('recE1', 'recV2')`)

	insertArticle := func(id, title string) {
		t.Helper()
		exec(`INSERT INTO external_articles
		      (id, wikidata_id, title, description, extract, url, revision, retrieved, license)
		      VALUES (?, ?, ?, '', 'Quoted opening.', 'https://en.wikipedia.org/wiki/X', 1, '2026-01-01T00:00:00Z', 'cc-by-sa-4')`,
			id, id, title)
	}

	insertArticle("q1", "Necho II")
	insertArticle("q2", "Enuma Elis")
	insertArticle("q3", "Kingdom of Judah")

	// Deliberately inserted parallel-first so a passing ordering assertion
	// cannot be an accident of insertion order.
	exec(`INSERT INTO external_article_links (article_id, scope, target_id, kind, relevance, rank)
	      VALUES ('q2', 'event', 'recE1', 'parallel', 'A comparison text', 0)`)
	exec(`INSERT INTO external_article_links (article_id, scope, target_id, kind, relevance, rank)
	      VALUES ('q1', 'event', 'recE1', 'history', 'Pharaoh who deposed him', 1)`)
	exec(`INSERT INTO external_article_links (article_id, scope, target_id, kind, relevance, rank)
	      VALUES ('q3', 'book', 'bk2kings', 'history', 'The kingdom this book covers', 0)`)

	return path
}

func TestGetExternalContext(t *testing.T) {
	path := externalFixture(t)

	db, err := OpenBibleDB(path)
	if err != nil {
		t.Fatalf("OpenBibleDB: %v", err)
	}
	defer db.Close()

	t.Run("verse inside the event gets both its articles and its book's", func(t *testing.T) {
		articles, err := GetExternalContext(db, "2KI.23.31")
		if err != nil {
			t.Fatalf("GetExternalContext: %v", err)
		}

		if len(articles) != 3 {
			t.Fatalf("want 3 articles, got %d: %+v", len(articles), titlesOf(articles))
		}

		// History before parallels, and within history the event-scoped article
		// before the book-scoped one: the reader should meet the most specific
		// material first.
		want := []string{"Necho II", "Kingdom of Judah", "Enuma Elis"}
		for i, title := range want {
			if articles[i].Title != title {
				t.Errorf("position %d: want %q, got %q (full order %v)", i, title, articles[i].Title, titlesOf(articles))
			}
		}

		if articles[0].Kind != "history" || articles[0].Scope != "event" {
			t.Errorf("Necho II: want history/event, got %s/%s", articles[0].Kind, articles[0].Scope)
		}
		if articles[0].Relevance != "Pharaoh who deposed him" {
			t.Errorf("relevance not carried through: %q", articles[0].Relevance)
		}
	})

	t.Run("verse outside the event still gets its book's articles", func(t *testing.T) {
		articles, err := GetExternalContext(db, "2KI.24.1")
		if err != nil {
			t.Fatalf("GetExternalContext: %v", err)
		}

		if len(articles) != 1 || articles[0].Title != "Kingdom of Judah" {
			t.Fatalf("want only the book article, got %v", titlesOf(articles))
		}
		if articles[0].Scope != "book" {
			t.Errorf("want book scope, got %q", articles[0].Scope)
		}
	})

	t.Run("verse in an unlinked book gets nothing", func(t *testing.T) {
		articles, err := GetExternalContext(db, "GEN.1.1")
		if err != nil {
			t.Fatalf("GetExternalContext: %v", err)
		}

		if len(articles) != 0 {
			t.Fatalf("want none, got %v", titlesOf(articles))
		}
	})
}

func titlesOf(articles []ExternalArticle) []string {
	titles := make([]string, 0, len(articles))
	for _, a := range articles {
		titles = append(titles, a.Title)
	}
	return titles
}
