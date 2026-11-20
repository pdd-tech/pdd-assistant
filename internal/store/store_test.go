package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

const testPassword = "test-only-password"

func openTestStore(t *testing.T, path string) *Store {
	t.Helper()
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestDataPersistsAndAdminPasswordIsPreserved(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "данные #+.sqlite")
	first := openTestStore(t, path)
	if err := first.EnsureAdmin(ctx, "admin", testPassword); err != nil {
		t.Fatal(err)
	}
	if err := first.CreateStudent(ctx, " ученик ", testPassword); err != nil {
		t.Fatal(err)
	}
	student, err := first.Authenticate(ctx, "ученик", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if student.Role != RoleStudent || student.CreatedAt.IsZero() {
		t.Fatalf("unexpected student: %+v", student)
	}

	for _, question := range []string{"первый вопрос", "вопрос с 'кавычками'"} {
		if err := first.LogQuestion(ctx, student.ID, question); err != nil {
			t.Fatal(err)
		}
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	db := openTestStore(t, path)
	if err := db.EnsureAdmin(ctx, "admin", "new-test-password"); err != nil {
		t.Fatal(err)
	}
	admin, err := db.Authenticate(ctx, "admin", testPassword)
	if err != nil || admin.Role != RoleAdmin {
		t.Fatalf("authenticate original admin: %+v, %v", admin, err)
	}
	if _, err := db.Authenticate(ctx, "admin", "new-test-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("authenticate replacement password: %v", err)
	}
	students, err := db.ListStudents(ctx)
	if err != nil || len(students) != 1 || students[0].ID != student.ID {
		t.Fatalf("list students: %+v, %v", students, err)
	}
	questions, err := db.ListQueries(ctx)
	if err != nil || len(questions) != 2 {
		t.Fatalf("list queries: %+v, %v", questions, err)
	}
	if questions[0].Question != "вопрос с 'кавычками'" ||
		questions[1].Question != "первый вопрос" ||
		questions[0].Login != "ученик" || questions[0].CreatedAt.IsZero() {
		t.Fatalf("wrong question order or data: %+v", questions)
	}
}

func TestStudentLoginCollision(t *testing.T) {
	ctx := context.Background()
	db := openTestStore(t, filepath.Join(t.TempDir(), "test.sqlite"))
	if err := db.CreateStudent(ctx, "same", testPassword); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateStudent(ctx, " same ", testPassword); !errors.Is(err, ErrDuplicateLogin) {
		t.Fatalf("duplicate student: %v", err)
	}
	if err := db.EnsureAdmin(ctx, "same", testPassword); !errors.Is(err, ErrDuplicateLogin) {
		t.Fatalf("admin seed on student login: %v", err)
	}
	student, err := db.Authenticate(ctx, "same", testPassword)
	if err != nil || student.Role != RoleStudent {
		t.Fatalf("student role changed: %+v, %v", student, err)
	}
}

func TestStudentPasswordStoredAsBcrypt(t *testing.T) {
	ctx := context.Background()
	db := openTestStore(t, filepath.Join(t.TempDir(), "test.sqlite"))
	if err := db.CreateStudent(ctx, "student", testPassword); err != nil {
		t.Fatal(err)
	}
	var hash string
	err := db.db.QueryRowContext(ctx, "SELECT password_hash FROM users WHERE login = ?", "student").Scan(&hash)
	if err != nil {
		t.Fatal(err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(testPassword)); err != nil {
		t.Fatalf("password wasn't stored as bcrypt: %v", err)
	}
}

func TestQuestionCannotReferenceMissingAccount(t *testing.T) {
	ctx := context.Background()
	db := openTestStore(t, filepath.Join(t.TempDir(), "test.sqlite"))
	db.db.SetMaxIdleConns(0)
	if err := db.LogQuestion(ctx, 123, "orphan question"); err == nil {
		t.Fatal("foreign key was not enforced on a new connection")
	}
	questions, err := db.ListQueries(ctx)
	if err != nil || len(questions) != 0 {
		t.Fatalf("unexpected journal: %+v, %v", questions, err)
	}
}

func TestCreateStudentRejectsInvalidCredentials(t *testing.T) {
	ctx := context.Background()
	db := openTestStore(t, filepath.Join(t.TempDir(), "test.sqlite"))
	for _, tc := range []struct {
		name, login, password string
	}{
		{"empty login", "   ", testPassword},
		{"space in login", "first name", testPassword},
		{"control in login", "a\x00b", testPassword},
		{"long login", strings.Repeat("я", 65), testPassword},
		{"empty password", "student", " \t"},
		{"long UTF-8 password", "student", strings.Repeat("я", 37)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := db.CreateStudent(ctx, tc.login, tc.password); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("invalid credentials: %v", err)
			}
		})
	}
}
