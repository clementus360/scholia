package corpus

import (
	"database/sql"
	"fmt"
)

// batchSize is how many rows accumulate before a commit.
//
// The seeders originally wrapped an entire dataset in one transaction —
// 446,920 analysis tokens in one, 606,790 cross-references in another. SQLite
// holds every dirty page of an open transaction in memory until commit, and
// with the WASM driver that memory lives in wazero's linear address space,
// outside the Go heap. GOMEMLIMIT therefore cannot restrain it, and the build
// dies on any machine without a lot of headroom while working fine on a
// developer laptop.
//
// Committing periodically bounds that. 25k is large enough that per-transaction
// overhead stays negligible and small enough to keep the resident set flat.
const batchSize = 25_000

// batcher commits an in-progress transaction every batchSize rows, transparently
// starting the next one.
//
// This deliberately gives up all-or-nothing seeding. It is the right trade here:
// the corpus is built into a temporary file and only renamed into place once the
// whole run succeeds (see Build), so a partial database is discarded rather than
// served. Atomicity at the transaction level buys nothing on top of that.
type batcher struct {
	db    *sql.DB
	tx    *sql.Tx
	count int
}

func newBatcher(db *sql.DB) (*batcher, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	return &batcher{db: db, tx: tx}, nil
}

// Tx returns the current transaction. Re-read it after every Step, because a
// commit replaces it.
func (b *batcher) Tx() *sql.Tx { return b.tx }

// Step records that a row was written and rotates the transaction when the
// batch is full.
func (b *batcher) Step() error {
	b.count++
	if b.count < batchSize {
		return nil
	}
	b.count = 0

	if err := b.tx.Commit(); err != nil {
		return fmt.Errorf("commit batch: %w", err)
	}

	tx, err := b.db.Begin()
	if err != nil {
		return fmt.Errorf("begin next batch: %w", err)
	}
	b.tx = tx
	return nil
}

// Done commits whatever remains.
func (b *batcher) Done() error {
	if err := b.tx.Commit(); err != nil {
		return fmt.Errorf("commit final batch: %w", err)
	}
	return nil
}
