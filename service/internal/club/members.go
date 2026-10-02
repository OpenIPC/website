// Package club is the OpenIPC Club: signing in to openipc.org with Telegram,
// GitHub or an emailed link, to follow what one sent to the board catalogue,
// keep one's flash dumps private to oneself and the maintainers, and collect
// stars for what is accepted (migration 019).
//
// It holds accounts and sessions and nothing else. Every row about a report
// -- who sent it, its files, its review, its stars -- is owner reports'
// (internal/reports), which this package calls and never queries.
//
// The session cookie's path is /api/v1/club: the pages are static and cached
// for everyone, a visitor who never signs in is never sent a cookie, and the
// signed-in browser shows who it is by asking /api/v1/club/me.
package club

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type execer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

const (
	sessionDays = 90
	loginTTL    = 10 * time.Minute
)

// Member is a signed-in person, as /api/v1/club/me shows them.
type Member struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Maintainer bool       `json:"maintainer"`
	Quiet      bool       `json:"quiet"`
	Identities []Identity `json:"identities"`
	Stars      int        `json:"stars"`
	Pending    int        `json:"pending"`
}

type Identity struct {
	Provider string `json:"provider"`
	Handle   string `json:"handle"`
	// Telegram: the bot can write to them
	Chat bool `json:"chat,omitempty"`
}

// signIn is one way in, completed: who the provider says this is.
type signIn struct {
	Provider   string
	Subject    string
	Handle     string
	Name       string
	Maintainer bool
	ChatID     *int64
	Locale     string
}

func randomString(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func sha(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func newMemberID() string {
	const alphabet = "abcdefghijkmnpqrstuvwxyz23456789"
	var b [10]byte
	_, _ = rand.Read(b[:])
	out := []byte("m-")
	for _, c := range b {
		out = append(out, alphabet[int(c)%len(alphabet)])
	}
	return string(out)
}

// cleanName is a display name as a member may carry it: one line, 1-80
// characters, never empty.
func cleanName(s string) string {
	s = strings.Join(strings.Fields(strings.ToValidUTF8(s, "")), " ")
	for utf8.RuneCountInString(s) > 80 {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
	}
	if s == "" {
		s = "OpenIPC member"
	}
	return s
}

// identify finds or makes the member an identity belongs to. An identity
// already known is its member's, whoever asks; a new one joins forMember
// when the asking browser was signed in, and is a new member otherwise.
func identify(ctx context.Context, tx pgx.Tx, in signIn, forMember string) (string, error) {
	var member string
	err := tx.QueryRow(ctx, `
		UPDATE club_identities SET handle = $3, maintainer = $4,
		  chat_id = coalesce($5, chat_id), locale = coalesce(nullif($6, ''), locale), seen_at = now()
		WHERE provider = $1 AND subject = $2 RETURNING member_id`,
		in.Provider, in.Subject, in.Handle, in.Maintainer, in.ChatID, in.Locale).Scan(&member)
	if err == nil {
		return member, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	member = forMember
	if member == "" {
		member = newMemberID()
		if _, err := tx.Exec(ctx, `INSERT INTO club_members (id, name) VALUES ($1, $2)`, member, cleanName(in.Name)); err != nil {
			return "", err
		}
	}
	locale := in.Locale
	if locale == "" {
		locale = "en"
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO club_identities (provider, subject, member_id, handle, maintainer, chat_id, locale)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		in.Provider, in.Subject, member, in.Handle, in.Maintainer, in.ChatID, locale)
	return member, err
}

// newSession signs a browser in: the token goes in its cookie, only the
// token's sha256 is stored.
func (a *API) newSession(ctx context.Context, q execer, member string) (string, error) {
	token := randomString(32)
	_, err := q.Exec(ctx, `INSERT INTO club_sessions (token_sha256, member_id, expires_at) VALUES ($1, $2, $3)`,
		sha(token), member, a.now().Add(sessionDays*24*time.Hour))
	return token, err
}

// member reads one member whole, with their identities and stars.
func (a *API) member(ctx context.Context, id string) (*Member, error) {
	m := &Member{ID: id, Identities: []Identity{}}
	if err := a.DB.QueryRow(ctx, `SELECT name, quiet FROM club_members WHERE id = $1`, id).Scan(&m.Name, &m.Quiet); err != nil {
		return nil, err
	}
	rows, err := a.DB.Query(ctx, `
		SELECT provider, handle, maintainer, chat_id IS NOT NULL FROM club_identities
		WHERE member_id = $1 ORDER BY provider`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var i Identity
		var maint bool
		if err := rows.Scan(&i.Provider, &i.Handle, &maint, &i.Chat); err != nil {
			rows.Close()
			return nil, err
		}
		m.Maintainer = m.Maintainer || maint
		m.Identities = append(m.Identities, i)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, x := range a.Cfg.Maintainers {
		m.Maintainer = m.Maintainer || x == id
	}
	m.Stars, m.Pending, err = a.Reports.Store().StarsOf(ctx, id)
	return m, err
}

// Purge drops what the nightly purge may: sessions past their expiry, and
// sign-ins a day after theirs (kept that long so a late Start still finds
// its code, and says it expired rather than that it never existed).
func Purge(ctx context.Context, db execer) (sessions, logins int64, err error) {
	tag, err := db.Exec(ctx, `DELETE FROM club_sessions WHERE expires_at < now()`)
	if err != nil {
		return 0, 0, err
	}
	sessions = tag.RowsAffected()
	tag, err = db.Exec(ctx, `DELETE FROM club_logins WHERE expires_at < now() - interval '1 day'`)
	return sessions, tag.RowsAffected(), err
}
