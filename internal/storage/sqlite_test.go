package storage

import (
	"os"
	"path/filepath"
	"testing"
)

// seedFixture builds a small corpus the way cmd/seed does and returns its path.
func seedFixture(t *testing.T, finalize bool) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "bible.db")

	db, err := OpenBibleDBForSeed(path)
	if err != nil {
		t.Fatalf("OpenBibleDBForSeed: %v", err)
	}

	if err := CreateBibleTables(db); err != nil {
		t.Fatalf("CreateBibleTables: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO verses (id, translation, book, chapter, verse, text) VALUES (?, ?, ?, ?, ?, ?)`,
		"BSB.GEN.1.1", "BSB", "GEN", 1, 1, "In the beginning...",
	); err != nil {
		t.Fatalf("insert verse: %v", err)
	}

	if finalize {
		if err := FinalizeBibleDB(db); err != nil {
			t.Fatalf("FinalizeBibleDB: %v", err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close seed handle: %v", err)
	}

	return path
}

// TestFinalizeLeavesNoSidecars guards the constraint that makes the read-only
// deployment work at all: the shipped corpus must be a single self-contained
// file. A leftover -wal or -shm means the API cannot open it from a read-only
// image layer.
func TestFinalizeLeavesNoSidecars(t *testing.T) {
	path := seedFixture(t, true)

	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(path + suffix); !os.IsNotExist(err) {
			t.Errorf("expected no %s sidecar after finalize, but it exists", suffix)
		}
	}
}

func TestOpenBibleDBReadsFinalizedCorpus(t *testing.T) {
	path := seedFixture(t, true)

	db, err := OpenBibleDB(path)
	if err != nil {
		t.Fatalf("OpenBibleDB: %v", err)
	}
	defer db.Close()

	var text string
	if err := db.QueryRow(`SELECT text FROM verses WHERE id = ?`, "BSB.GEN.1.1").Scan(&text); err != nil {
		t.Fatalf("read verse: %v", err)
	}
	if text != "In the beginning..." {
		t.Errorf("got %q, want %q", text, "In the beginning...")
	}
}

// TestOpenBibleDBIsReadOnly confirms the handle cannot mutate the corpus, so a
// bug in a query path cannot corrupt the image's copy.
func TestOpenBibleDBIsReadOnly(t *testing.T) {
	path := seedFixture(t, true)

	db, err := OpenBibleDB(path)
	if err != nil {
		t.Fatalf("OpenBibleDB: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`DELETE FROM verses`); err == nil {
		t.Fatal("expected write to a read-only database to fail, but it succeeded")
	}
}

// TestOpenBibleDBRejectsStrayWAL covers the failure this whole arrangement
// exists to avoid. Opening a WAL-mode database read-only would fail obscurely
// at query time on a read-only filesystem; refusing up front names the fix.
func TestOpenBibleDBRejectsStrayWAL(t *testing.T) {
	path := seedFixture(t, false) // no finalize: -wal survives

	if _, err := os.Stat(path + "-wal"); os.IsNotExist(err) {
		t.Skip("driver checkpointed on close; nothing to assert")
	}

	if _, err := OpenBibleDB(path); err == nil {
		t.Fatal("expected OpenBibleDB to reject a corpus with an un-checkpointed WAL")
	}
}

// TestOpenBibleDBMissingFile ensures a missing corpus is an error rather than
// SQLite silently creating an empty database and the API serving zero verses.
func TestOpenBibleDBMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.db")

	if _, err := OpenBibleDB(path); err == nil {
		t.Fatal("expected an error for a missing corpus")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("OpenBibleDB must not create the database file")
	}
}
