package storage

import (
	"path/filepath"
	"testing"
)

// referenceFixture builds a corpus holding one verse for each book whose code
// is easy to get wrong, so reference resolution can be tested without the real
// 100MB corpus being present.
func referenceFixture(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "bible.db")

	db, err := OpenBibleDBForSeed(path)
	if err != nil {
		t.Fatalf("OpenBibleDBForSeed: %v", err)
	}
	if err := CreateBibleTables(db); err != nil {
		t.Fatalf("CreateBibleTables: %v", err)
	}

	// Book names are spelled as the BSB source spells them, because that is
	// what the seeder writes and what resolution matches against.
	verses := []struct {
		id, book string
	}{
		{"1JO.1.1", "1 John"},
		{"2JO.1.1", "2 John"},
		{"3JO.1.1", "3 John"},
		{"JHN.1.1", "John"},
		{"1KI.1.1", "1 Kings"},
		{"PSA.23.1", "Psalms"},
		{"MAT.1.1", "Matthew"},
		{"JUD.1.3", "Jude"},
		{"OBA.1.5", "Obadiah"},
		{"PHM.1.6", "Philemon"},
	}

	for _, verse := range verses {
		if _, err := db.Exec(
			`INSERT INTO verses (id, translation, book, chapter, verse, text) VALUES (?, 'BSB', ?, ?, ?, 'text')`,
			verse.id, verse.book, 1, 1,
		); err != nil {
			t.Fatalf("insert %s: %v", verse.id, err)
		}
	}

	// Every row above went in at chapter 1, verse 1. Correct the ones whose id
	// says otherwise, so a test asserting "Jude 3" resolves is not satisfied by
	// a verse that only exists at 1:1.
	for id, cv := range map[string][2]int{
		"PSA.23.1": {23, 1},
		"JUD.1.3":  {1, 3},
		"OBA.1.5":  {1, 5},
		"PHM.1.6":  {1, 6},
	} {
		if _, err := db.Exec(
			`UPDATE verses SET chapter = ?, verse = ? WHERE id = ?`, cv[0], cv[1], id); err != nil {
			t.Fatalf("fix %s: %v", id, err)
		}
	}

	if err := FinalizeBibleDB(db); err != nil {
		t.Fatalf("FinalizeBibleDB: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	return path
}

// TestReferenceResolvesBookCodeAliases covers the codes callers actually send.
//
// The Johannine letters are the reason this test exists: the frontend and the
// USFM standard both call them 1JN/2JN/3JN, while the corpus stores 1JO/2JO/3JO
// after the BSB book names. Nothing rejected the mismatch — 1 John simply
// resolved to no verse and vanished from the UI.
func TestReferenceResolvesBookCodeAliases(t *testing.T) {
	db, err := OpenBibleDB(referenceFixture(t))
	if err != nil {
		t.Fatalf("OpenBibleDB: %v", err)
	}
	defer db.Close()

	cases := []struct{ reference, want string }{
		{"1JN.1.1", "1JO.1.1"},
		{"2JN.1.1", "2JO.1.1"},
		{"3JN.1.1", "3JO.1.1"},
		{"1JO.1.1", "1JO.1.1"},
		{"1 John 1:1", "1JO.1.1"},
		{"1John.1.1", "1JO.1.1"},
		{"JHN.1.1", "JHN.1.1"},
		{"John 1:1", "JHN.1.1"},
		{"1KGS.1.1", "1KI.1.1"},
		{"1 Kings 1:1", "1KI.1.1"},
		{"PS.23.1", "PSA.23.1"},
		{"Psalm 23:1", "PSA.23.1"},
		{"MATT.1.1", "MAT.1.1"},

		// Single-chapter books are cited without their chapter by convention.
		{"Jude 3", "JUD.1.3"},
		{"JUD.3", "JUD.1.3"},
		{"Jude 1:3", "JUD.1.3"},
		{"Obadiah 5", "OBA.1.5"},
		{"OBA.5", "OBA.1.5"},
		{"Philemon 6", "PHM.1.6"},
		// A numbered single-chapter book exercises both fixes at once: the
		// 2JN -> 2JO alias and the missing chapter.
		{"2 John 1", "2JO.1.1"},
		{"2JN.1", "2JO.1.1"},
	}

	for _, tc := range cases {
		result, err := GetVerseRangeByReference(db, tc.reference)
		if err != nil {
			t.Errorf("%s: unexpected error %v", tc.reference, err)
			continue
		}
		if result == nil || len(result.Verses) == 0 {
			t.Errorf("%s: resolved to nothing, want %s", tc.reference, tc.want)
			continue
		}
		if got := result.Verses[0].ID; got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.reference, got, tc.want)
		}
	}
}
