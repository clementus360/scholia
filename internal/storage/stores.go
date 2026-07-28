package storage

import "database/sql"

// Stores holds the two backing databases. They are deliberately separate:
//
//   - Bible is a read-only SQLite file rebuilt from data/ by cmd/seed and baked
//     into the container image. It is disposable; throwing it away and
//     regenerating it costs nothing.
//   - Users is Supabase Postgres and holds everything a person creates. It must
//     survive deploys.
//
// These used to be one SQLite file, which meant rebuilding the corpus (or a
// plain `git checkout`, since the file was committed) destroyed user accounts
// and notes. Keep them apart.
type Stores struct {
	Bible *sql.DB
	Users *sql.DB
}

// Close releases both handles, returning the first error encountered.
func (s *Stores) Close() error {
	var firstErr error
	for _, db := range []*sql.DB{s.Bible, s.Users} {
		if db == nil {
			continue
		}
		if err := db.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
