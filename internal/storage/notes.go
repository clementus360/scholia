package storage

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// Note is a user-authored study note. It lives in Postgres, not in the Bible
// SQLite file.
//
// OwnerUserID is a Supabase Auth user UUID. It is never serialized: clients
// only ever see their own notes, so echoing the owner back adds nothing and
// leaks an internal identifier.
type Note struct {
	ID            int64     `json:"id"`
	OwnerUserID   string    `json:"-"`
	Title         string    `json:"title"`
	MainReference string    `json:"main_reference"`
	Content       string    `json:"content"`
	VerseIDs      []string  `json:"verse_ids,omitempty"`
	CreatedAt     time.Time `json:"created_at,omitzero"`
	UpdatedAt     time.Time `json:"updated_at,omitzero"`
}

// noteExecutor is satisfied by both *sql.DB and *sql.Tx, letting the verse-link
// helpers run inside or outside a transaction.
type noteExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// owner_user_id is a Postgres uuid and is selected as ::text so it scans into a
// plain Go string. Likewise every uuid parameter below is written as $n::uuid —
// without the cast Postgres rejects the comparison with
// "operator does not exist: uuid = text".
const noteColumns = `id, owner_user_id::text, title, main_reference, content, created_at, updated_at`

func scanNote(scanner interface{ Scan(...any) error }) (Note, error) {
	var note Note
	if err := scanner.Scan(
		&note.ID, &note.OwnerUserID, &note.Title, &note.MainReference,
		&note.Content, &note.CreatedAt, &note.UpdatedAt,
	); err != nil {
		return Note{}, err
	}
	return note, nil
}

func collectNotes(rows *sql.Rows) ([]Note, error) {
	defer rows.Close()

	notes := make([]Note, 0)
	for rows.Next() {
		note, err := scanNote(rows)
		if err != nil {
			return nil, err
		}
		notes = append(notes, note)
	}
	return notes, rows.Err()
}

// GetNotesByVerseID returns the caller's notes attached to a given verse.
//
// Note that this touches only the notes and note_verses tables — it never joins
// against the verses table, which is why notes could be moved to Postgres
// without splitting a query in half.
func GetNotesByVerseID(ctx context.Context, db *sql.DB, ownerID, verseID string, limit, offset int) ([]Note, error) {
	if strings.TrimSpace(ownerID) == "" {
		return []Note{}, nil
	}

	rows, err := db.QueryContext(ctx, `
		SELECT n.id, n.owner_user_id::text, n.title, n.main_reference, n.content, n.created_at, n.updated_at
		FROM notes n
		INNER JOIN note_verses nv ON nv.note_id = n.id
		WHERE nv.verse_id = $1 AND n.owner_user_id = $2::uuid
		ORDER BY n.updated_at DESC, n.id DESC
		LIMIT $3 OFFSET $4`, verseID, ownerID, limit, offset)
	if err != nil {
		return nil, err
	}
	return collectNotes(rows)
}

func ListNotes(ctx context.Context, db *sql.DB, ownerID string, limit, offset int) ([]Note, error) {
	if strings.TrimSpace(ownerID) == "" {
		return []Note{}, nil
	}

	rows, err := db.QueryContext(ctx, `
		SELECT `+noteColumns+`
		FROM notes
		WHERE owner_user_id = $1::uuid
		ORDER BY updated_at DESC, id DESC
		LIMIT $2 OFFSET $3`, ownerID, limit, offset)
	if err != nil {
		return nil, err
	}
	return collectNotes(rows)
}

// GetNoteByID returns a single note, or (nil, nil) when it does not exist or
// belongs to someone else. Both cases are deliberately indistinguishable to the
// caller so the API cannot be used to probe for other users' note IDs.
func GetNoteByID(ctx context.Context, db *sql.DB, ownerID string, noteID int64) (*Note, error) {
	if strings.TrimSpace(ownerID) == "" {
		return nil, nil
	}

	row := db.QueryRowContext(ctx, `
		SELECT `+noteColumns+`
		FROM notes
		WHERE id = $1 AND owner_user_id = $2::uuid`, noteID, ownerID)

	note, err := scanNote(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	verseIDs, err := getNoteVerseIDs(ctx, db, note.ID)
	if err != nil {
		return nil, err
	}
	note.VerseIDs = verseIDs
	return &note, nil
}

func CreateNote(ctx context.Context, db *sql.DB, note *Note) (int64, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// Postgres has no LastInsertId; the generated key comes back via RETURNING.
	var noteID int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO notes (owner_user_id, title, main_reference, content)
		VALUES ($1::uuid, $2, $3, $4)
		RETURNING id`,
		note.OwnerUserID, note.Title, note.MainReference, note.Content,
	).Scan(&noteID); err != nil {
		return 0, err
	}

	if err := replaceNoteVerses(ctx, tx, noteID, note.VerseIDs); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return noteID, nil
}

// UpdateNote returns sql.ErrNoRows when the note does not exist or is not owned
// by ownerID, which handlers translate into a 404.
func UpdateNote(ctx context.Context, db *sql.DB, ownerID string, note *Note) error {
	if strings.TrimSpace(ownerID) == "" {
		return sql.ErrNoRows
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// updated_at is maintained by the notes_touch_updated_at trigger.
	result, err := tx.ExecContext(ctx, `
		UPDATE notes
		SET title = $1, main_reference = $2, content = $3
		WHERE id = $4 AND owner_user_id = $5::uuid`,
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

	if err := replaceNoteVerses(ctx, tx, note.ID, note.VerseIDs); err != nil {
		return err
	}
	return tx.Commit()
}

func DeleteNote(ctx context.Context, db *sql.DB, ownerID string, noteID int64) error {
	if strings.TrimSpace(ownerID) == "" {
		return sql.ErrNoRows
	}

	// note_verses rows are removed by the ON DELETE CASCADE on note_verses.note_id,
	// so a single scoped delete is enough.
	result, err := db.ExecContext(ctx, `
		DELETE FROM notes WHERE id = $1 AND owner_user_id = $2::uuid`, noteID, ownerID)
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
	return nil
}

func getNoteVerseIDs(ctx context.Context, db noteExecutor, noteID int64) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT verse_id FROM note_verses WHERE note_id = $1 ORDER BY verse_id ASC`, noteID)
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
	return verseIDs, rows.Err()
}

// replaceNoteVerses resets a note's verse links to exactly verseIDs.
//
// Callers must have validated the references against the Bible database first
// (see ExpandVerseReferences) — note_verses.verse_id has no foreign key,
// because the verses table lives in a different database.
func replaceNoteVerses(ctx context.Context, db noteExecutor, noteID int64, verseIDs []string) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM note_verses WHERE note_id = $1`, noteID); err != nil {
		return err
	}

	for _, verseID := range verseIDs {
		if strings.TrimSpace(verseID) == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO note_verses (note_id, verse_id)
			VALUES ($1, $2)
			ON CONFLICT (note_id, verse_id) DO NOTHING`, noteID, verseID); err != nil {
			return err
		}
	}
	return nil
}
