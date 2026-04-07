package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrInviteNotFound       = errors.New("invite code not found")
	ErrInviteAlreadyRedeem  = errors.New("invite code already redeemed")
	ErrInviteInvalid        = errors.New("invite code invalid")
	ErrInviteCreationFailed = errors.New("invite code creation failed")
)

type InviteRedemption struct {
	Principal Principal
	APIKey    string
}

func CreateInviteCode(db *sql.DB, createdByUserID, label string, scopes []string) (string, string, error) {
	createdByUserID = strings.TrimSpace(createdByUserID)
	if createdByUserID == "" {
		return "", "", ErrInviteCreationFailed
	}
	label = strings.TrimSpace(label)
	if label == "" {
		label = "tester"
	}
	parsedScopes := parseScopes(strings.Join(scopes, ","))
	code := generateInviteCode()
	codeHash := hashInviteCode(code)
	inviteID := newID("inv")

	tx, err := db.Begin()
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		INSERT INTO invite_codes (id, code_hash, label, scopes, created_by_user_id)
		VALUES (?, ?, ?, ?, ?)`, inviteID, codeHash, label, strings.Join(parsedScopes, ","), createdByUserID); err != nil {
		return "", "", err
	}

	if err := tx.Commit(); err != nil {
		return "", "", err
	}

	return inviteID, code, nil
}

func RedeemInviteCode(db *sql.DB, rawCode string) (*InviteRedemption, error) {
	code := normalizeInviteCode(rawCode)
	if code == "" {
		return nil, ErrInviteInvalid
	}
	codeHash := hashInviteCode(code)

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var (
		inviteID   string
		label      string
		scopes     string
		consumedAt sql.NullString
	)
	err = tx.QueryRow(`
		SELECT id, label, scopes, consumed_at
		FROM invite_codes
		WHERE code_hash = ?
		LIMIT 1`, codeHash).Scan(&inviteID, &label, &scopes, &consumedAt)
	if err == sql.ErrNoRows {
		return nil, ErrInviteNotFound
	}
	if err != nil {
		return nil, err
	}
	if consumedAt.Valid && strings.TrimSpace(consumedAt.String) != "" {
		return nil, ErrInviteAlreadyRedeem
	}

	userID := newID("usr")
	keyID := newID("key")
	apiKey := generateAPIKeyToken()
	subject := fmt.Sprintf("invite-%s", inviteID)
	parsedScopes := parseScopes(scopes)

	if _, err := tx.Exec(`
		INSERT INTO users (id, subject, display_name, role)
		VALUES (?, ?, ?, ?)`, userID, subject, label, "member"); err != nil {
		return nil, err
	}

	if _, err := tx.Exec(`
		INSERT INTO api_keys (id, user_id, token_hash, label, scopes, active)
		VALUES (?, ?, ?, ?, ?, 1)`, keyID, userID, hashToken(apiKey), label, strings.Join(parsedScopes, ",")); err != nil {
		return nil, err
	}

	result, err := tx.Exec(`
		UPDATE invite_codes
		SET consumed_by_user_id = ?, consumed_api_key_id = ?, consumed_at = CURRENT_TIMESTAMP
		WHERE id = ? AND consumed_at IS NULL`, userID, keyID, inviteID)
	if err != nil {
		return nil, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if rowsAffected == 0 {
		return nil, ErrInviteAlreadyRedeem
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	principal := Principal{
		Type:           "api-key",
		UserID:         userID,
		KeyID:          keyID,
		Subject:        subject,
		DisplayName:    label,
		Scopes:         parsedScopes,
		Authenticated:  true,
		Authentication: "invite-code",
	}

	return &InviteRedemption{Principal: principal, APIKey: apiKey}, nil
}

func generateInviteCode() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("INV-%s", strings.ToUpper(newID("code")))
	}
	raw := strings.ToUpper(hex.EncodeToString(buf))
	parts := make([]string, 0, len(raw)/4)
	for i := 0; i < len(raw); i += 4 {
		end := i + 4
		if end > len(raw) {
			end = len(raw)
		}
		parts = append(parts, raw[i:end])
	}
	return strings.Join(parts, "-")
}

func generateAPIKeyToken() string {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return newID("tok")
	}
	return hex.EncodeToString(buf)
}

func normalizeInviteCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	code = strings.ReplaceAll(code, "-", "")
	code = strings.ReplaceAll(code, " ", "")
	return code
}

func hashInviteCode(code string) string {
	sum := sha256.Sum256([]byte(normalizeInviteCode(code)))
	return hex.EncodeToString(sum[:])
}
