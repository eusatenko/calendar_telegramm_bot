package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

var ErrLastAdmin = errors.New("нельзя деактивировать последнего администратора")
var ErrInviteInvalid = errors.New("приглашение недействительно или уже использовано")

type User struct {
	ID                        int64
	Username, FirstName, Role string
	Active                    bool
	CreatedAt                 time.Time
	CreatedBy                 sql.NullInt64
}
type Store struct {
	db  *sql.DB
	now func() time.Time
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("создание каталога БД: %w", err)
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, now: time.Now}
	if err = s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS schema_version(version INTEGER PRIMARY KEY);
INSERT OR IGNORE INTO schema_version(version) VALUES(1);
CREATE TABLE IF NOT EXISTS users(
 telegram_user_id INTEGER PRIMARY KEY, username TEXT, first_name TEXT,
 role TEXT NOT NULL CHECK(role IN ('admin','user')), is_active INTEGER NOT NULL CHECK(is_active IN (0,1)),
 created_at DATETIME NOT NULL, created_by INTEGER);
CREATE INDEX IF NOT EXISTS idx_users_active_role ON users(is_active,role);
CREATE TABLE IF NOT EXISTS invites(
 id INTEGER PRIMARY KEY AUTOINCREMENT, token_hash TEXT UNIQUE NOT NULL, created_by INTEGER NOT NULL,
 created_at DATETIME NOT NULL, expires_at DATETIME NOT NULL, used_at DATETIME, used_by INTEGER);
CREATE INDEX IF NOT EXISTS idx_invites_hash_expires ON invites(token_hash,expires_at);
CREATE TABLE IF NOT EXISTS audit_log(
 id INTEGER PRIMARY KEY AUTOINCREMENT, actor_user_id INTEGER, action TEXT NOT NULL,
 target_user_id INTEGER, created_at DATETIME NOT NULL, metadata TEXT);
CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_log(created_at);
`)
	return err
}
func (s *Store) BootstrapAdmin(id int64) error {
	_, err := s.db.Exec(`INSERT INTO users(telegram_user_id,role,is_active,created_at) VALUES(?,?,1,?)
ON CONFLICT(telegram_user_id) DO UPDATE SET role='admin',is_active=1`, id, "admin", s.now().UTC())
	return err
}
func (s *Store) GetUser(id int64) (User, error) {
	var u User
	var username, first sql.NullString
	err := s.db.QueryRow(`SELECT telegram_user_id,username,first_name,role,is_active,created_at,created_by FROM users WHERE telegram_user_id=?`, id).Scan(&u.ID, &username, &first, &u.Role, &u.Active, &u.CreatedAt, &u.CreatedBy)
	u.Username = username.String
	u.FirstName = first.String
	return u, err
}
func (s *Store) Authorized(id int64) (bool, bool, error) {
	u, e := s.GetUser(id)
	if errors.Is(e, sql.ErrNoRows) {
		return false, false, nil
	}
	return e == nil && u.Active, e == nil && u.Active && u.Role == "admin", e
}
func (s *Store) Touch(id int64, username, first string) error {
	_, e := s.db.Exec(`UPDATE users SET username=NULLIF(?,''),first_name=NULLIF(?,'') WHERE telegram_user_id=?`, username, first, id)
	return e
}
func (s *Store) ListUsers() ([]User, error) {
	rows, e := s.db.Query(`SELECT telegram_user_id,username,first_name,role,is_active,created_at,created_by FROM users ORDER BY role,telegram_user_id`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		var un, fn sql.NullString
		if e = rows.Scan(&u.ID, &un, &fn, &u.Role, &u.Active, &u.CreatedAt, &u.CreatedBy); e != nil {
			return nil, e
		}
		u.Username = un.String
		u.FirstName = fn.String
		out = append(out, u)
	}
	return out, rows.Err()
}
func (s *Store) AddUser(actor, id int64) error {
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = requireAdminTx(tx, actor); e != nil {
		return e
	}
	res, e := tx.Exec(`UPDATE users SET is_active=1,role=CASE WHEN role='admin' THEN role ELSE 'user' END WHERE telegram_user_id=?`, id)
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	action := "USER_REACTIVATED"
	if n == 0 {
		_, e = tx.Exec(`INSERT INTO users(telegram_user_id,role,is_active,created_at,created_by) VALUES(?,'user',1,?,?)`, id, s.now().UTC(), actor)
		action = "USER_ADDED"
	}
	if e != nil {
		return e
	}
	_, e = tx.Exec(`INSERT INTO audit_log(actor_user_id,action,target_user_id,created_at) VALUES(?,?,?,?)`, actor, action, id, s.now().UTC())
	if e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) SetActive(actor, id int64, active bool) error {
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = requireAdminTx(tx, actor); e != nil {
		return e
	}
	var role string
	var current bool
	if e = tx.QueryRow(`SELECT role,is_active FROM users WHERE telegram_user_id=?`, id).Scan(&role, &current); e != nil {
		return e
	}
	if !active && current && role == "admin" {
		var n int
		if e = tx.QueryRow(`SELECT COUNT(*) FROM users WHERE role='admin' AND is_active=1`).Scan(&n); e != nil {
			return e
		}
		if n <= 1 {
			return ErrLastAdmin
		}
	}
	_, e = tx.Exec(`UPDATE users SET is_active=? WHERE telegram_user_id=?`, active, id)
	if e != nil {
		return e
	}
	action := "USER_DEACTIVATED"
	if active {
		action = "USER_REACTIVATED"
	}
	_, e = tx.Exec(`INSERT INTO audit_log(actor_user_id,action,target_user_id,created_at) VALUES(?,?,?,?)`, actor, action, id, s.now().UTC())
	if e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) CreateInvite(actor int64, ttl time.Duration) (string, error) {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	hash := hashToken(token)
	now := s.now().UTC()
	tx, e := s.db.Begin()
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	if e = requireAdminTx(tx, actor); e != nil {
		return "", e
	}
	_, e = tx.Exec(`INSERT INTO invites(token_hash,created_by,created_at,expires_at) VALUES(?,?,?,?)`, hash, actor, now, now.Add(ttl))
	if e != nil {
		return "", e
	}
	_, e = tx.Exec(`INSERT INTO audit_log(actor_user_id,action,created_at) VALUES(?,'INVITE_CREATED',?)`, actor, now)
	if e != nil {
		return "", e
	}
	if e = tx.Commit(); e != nil {
		return "", e
	}
	return token, nil
}
func (s *Store) RedeemInvite(token string, userID int64, username, first string) error {
	if len(token) < 32 || len(token) > 128 {
		return ErrInviteInvalid
	}
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	now := s.now().UTC()
	hash := hashToken(token)
	var inviteID, creator int64
	var expires time.Time
	var used sql.NullTime
	e = tx.QueryRow(`SELECT id,created_by,expires_at,used_at FROM invites WHERE token_hash=?`, hash).Scan(&inviteID, &creator, &expires, &used)
	if errors.Is(e, sql.ErrNoRows) || used.Valid || !expires.After(now) {
		return ErrInviteInvalid
	}
	if e != nil {
		return e
	}
	res, e := tx.Exec(`UPDATE invites SET used_at=?,used_by=? WHERE id=? AND used_at IS NULL AND expires_at>?`, now, userID, inviteID, now)
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrInviteInvalid
	}
	_, e = tx.Exec(`INSERT INTO users(telegram_user_id,username,first_name,role,is_active,created_at,created_by) VALUES(?,?,?,'user',1,?,?) ON CONFLICT(telegram_user_id) DO UPDATE SET username=excluded.username,first_name=excluded.first_name,is_active=1`, userID, nullString(username), nullString(first), now, creator)
	if e != nil {
		return e
	}
	_, e = tx.Exec(`INSERT INTO audit_log(actor_user_id,action,target_user_id,created_at) VALUES(?,'INVITE_USED',?,?)`, creator, userID, now)
	if e != nil {
		return e
	}
	return tx.Commit()
}
func hashToken(t string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(t))) }
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func (s *Store) RequireAdmin(ctx context.Context, id int64) error {
	var n int
	e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE telegram_user_id=? AND role='admin' AND is_active=1`, id).Scan(&n)
	if e != nil {
		return e
	}
	if n != 1 {
		return errors.New("admin access required")
	}
	return nil
}

func requireAdminTx(tx *sql.Tx, id int64) error {
	var n int
	if e := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE telegram_user_id=? AND role='admin' AND is_active=1`, id).Scan(&n); e != nil {
		return e
	}
	if n != 1 {
		return errors.New("admin access required")
	}
	return nil
}
