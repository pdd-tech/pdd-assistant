// Package store persists users and their question history in SQLite.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

const (
	RoleAdmin   = "admin"
	RoleStudent = "student"
)

var (
	ErrInvalidInput       = errors.New("некорректные логин или пароль")
	ErrDuplicateLogin     = errors.New("логин уже занят")
	ErrInvalidCredentials = errors.New("неверный логин или пароль")
)

const schema = `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY,
		login TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		role TEXT NOT NULL CHECK (role IN ('admin', 'student')),
		created_at DATETIME NOT NULL
	);
	CREATE TABLE IF NOT EXISTS query_log (
		id INTEGER PRIMARY KEY,
		user_id INTEGER NOT NULL REFERENCES users(id),
		question TEXT NOT NULL,
		created_at DATETIME NOT NULL
	);
`

type User struct {
	ID        int64
	Login     string
	Role      string
	CreatedAt time.Time
}

type Query struct {
	Login     string
	Question  string
	CreatedAt time.Time
}

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	location := url.URL{Scheme: "file"}
	if path == ":memory:" {
		location.Opaque = path
	} else {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("путь SQLite: %w", err)
		}
		location.Path = filepath.ToSlash(absolute)
		if !strings.HasPrefix(location.Path, "/") {
			location.Path = "/" + location.Path
		}
	}
	location.RawQuery = url.Values{
		"_pragma": {"foreign_keys(1)", "busy_timeout(5000)"},
	}.Encode()

	db, err := sql.Open("sqlite", location.String())
	if err != nil {
		return nil, fmt.Errorf("открытие SQLite: %w", err)
	}
	// Serialize SQLite operations in this process. DSN pragmas also apply
	// when database/sql replaces the underlying connection.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("схема SQLite: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func ValidateCredentials(login, password string) error {
	login = strings.TrimSpace(login)
	if login == "" || !utf8.ValidString(login) || utf8.RuneCountInString(login) > 64 ||
		strings.ContainsFunc(login, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return fmt.Errorf("%w: логин должен содержать от 1 до 64 символов без пробельных и управляющих символов", ErrInvalidInput)
	}
	if strings.TrimSpace(password) == "" || len(password) > 72 {
		return fmt.Errorf("%w: пароль должен содержать непробельный символ и занимать до 72 байт", ErrInvalidInput)
	}
	return nil
}

func (s *Store) EnsureAdmin(ctx context.Context, login, password string) error {
	err := s.createUser(ctx, login, password, RoleAdmin)
	if !errors.Is(err, ErrDuplicateLogin) {
		return err
	}

	var role string
	if err := s.db.QueryRowContext(ctx, "SELECT role FROM users WHERE login = ?", strings.TrimSpace(login)).Scan(&role); err != nil {
		return err
	}
	if role != RoleAdmin {
		return fmt.Errorf("ADMIN_LOGIN принадлежит ученику: %w", ErrDuplicateLogin)
	}
	return nil
}

func (s *Store) CreateStudent(ctx context.Context, login, password string) error {
	return s.createUser(ctx, login, password, RoleStudent)
}

func (s *Store) createUser(ctx context.Context, login, password, role string) error {
	if err := ValidateCredentials(login, password); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO users (login, password_hash, role, created_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(login) DO NOTHING`,
		strings.TrimSpace(login), string(hash), role, time.Now().UTC())
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrDuplicateLogin
	}
	return nil
}

func (s *Store) Authenticate(ctx context.Context, login, password string) (User, error) {
	if err := ValidateCredentials(login, password); err != nil {
		return User{}, ErrInvalidCredentials
	}
	var user User
	var hash []byte
	err := s.db.QueryRowContext(ctx, `
		SELECT id, login, role, created_at, password_hash FROM users WHERE login = ?`,
		strings.TrimSpace(login)).
		Scan(&user.ID, &user.Login, &user.Role, &user.CreatedAt, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		// Spend the same bcrypt work on a missing account as on a wrong password.
		const dummyHash = "$2a$10$XajjQvNhvvRt5GSeFk1xFeyqRrsxkhBkUiQeg0dt.wU1qD4aFDcga"
		_ = bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(password))
		return User{}, ErrInvalidCredentials
	}
	if err != nil {
		return User{}, err
	}
	if err := bcrypt.CompareHashAndPassword(hash, []byte(password)); err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return User{}, ErrInvalidCredentials
		}
		return User{}, fmt.Errorf("проверка хеша пароля: %w", err)
	}
	return user, nil
}

func (s *Store) ListStudents(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, login, role, created_at FROM users WHERE role = ?
		ORDER BY created_at DESC, id DESC`, RoleStudent)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var students []User
	for rows.Next() {
		var user User
		if err := rows.Scan(&user.ID, &user.Login, &user.Role, &user.CreatedAt); err != nil {
			return nil, err
		}
		students = append(students, user)
	}
	return students, rows.Err()
}

func (s *Store) LogQuestion(ctx context.Context, userID int64, question string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO query_log (user_id, question, created_at) VALUES (?, ?, ?)`,
		userID, question, time.Now().UTC())
	return err
}

func (s *Store) ListQueries(ctx context.Context) ([]Query, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT users.login, query_log.question, query_log.created_at
		FROM query_log JOIN users ON users.id = query_log.user_id
		ORDER BY query_log.created_at DESC, query_log.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var queries []Query
	for rows.Next() {
		var query Query
		if err := rows.Scan(&query.Login, &query.Question, &query.CreatedAt); err != nil {
			return nil, err
		}
		queries = append(queries, query)
	}
	return queries, rows.Err()
}
