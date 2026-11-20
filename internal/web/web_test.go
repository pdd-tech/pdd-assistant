package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/pdd-tech/pdd-assistant/internal/auth"
	"github.com/pdd-tech/pdd-assistant/internal/rag"
	"github.com/pdd-tech/pdd-assistant/internal/search"
	"github.com/pdd-tech/pdd-assistant/internal/store"
	"github.com/pdd-tech/pdd-assistant/internal/ui"
)

const (
	adminPassword   = "test-admin-secret"
	studentPassword = "test-student-secret"
)

type askFunc func(context.Context, string) (rag.Result, error)

func (f askFunc) Ask(ctx context.Context, question string) (rag.Result, error) {
	return f(ctx, question)
}

type browser struct {
	*http.Client
	base string
}

type page struct {
	Status int
	Header http.Header
	Body   string
}

func newBrowser(t *testing.T, asker askFunc) (*browser, *store.Store) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.EnsureAdmin(t.Context(), "test-admin", adminPassword); err != nil {
		t.Fatal(err)
	}
	renderer, err := ui.New()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(renderer, db, auth.New(false), asker, log.New(io.Discard, "", 0)))
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := server.Client()
	client.Jar = jar
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &browser{client, server.URL}, db
}

func (b *browser) request(t *testing.T, path string, form url.Values) page {
	t.Helper()
	var resp *http.Response
	var err error
	if form == nil {
		resp, err = b.Get(b.base + path)
	} else {
		resp, err = b.PostForm(b.base+path, form)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return page{Status: resp.StatusCode, Header: resp.Header, Body: string(body)}
}

var tokenPattern = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

func (p page) token(t *testing.T) string {
	t.Helper()
	match := tokenPattern.FindStringSubmatch(p.Body)
	if len(match) != 2 {
		t.Fatal("response did not have a CSRF token")
	}
	return match[1]
}

func (p page) check(t *testing.T, status int, text string) {
	t.Helper()
	if p.Status != status || !strings.Contains(p.Body, text) {
		t.Fatalf("HTTP %d, want %d and text %q", p.Status, status, text)
	}
}

func (p page) redirectTo(t *testing.T, path string) {
	t.Helper()
	if p.Status != http.StatusSeeOther || p.Header.Get("Location") != path {
		t.Fatalf("HTTP %d Location=%q, want 303 %q", p.Status, p.Header.Get("Location"), path)
	}
}

func credentials(login, password, token string) url.Values {
	return url.Values{"login": {login}, "password": {password}, "csrf_token": {token}}
}

func (b *browser) login(t *testing.T, login, password, target string) string {
	t.Helper()
	loginPage := b.request(t, "/login", nil)
	loginPage.check(t, http.StatusOK, "Вход в помощник")
	b.request(t, "/login", credentials(login, password, loginPage.token(t))).redirectTo(t, target)
	return b.request(t, target, nil).token(t)
}

func unusedAsker(context.Context, string) (rag.Result, error) {
	return rag.Result{}, errors.New("unexpected generation")
}

func TestLoginCreateStudentAndRoleIsolation(t *testing.T) {
	b, _ := newBrowser(t, unusedAsker)
	b.request(t, "/", nil).redirectTo(t, "/login")

	loginPage := b.request(t, "/login", nil)
	wrong := b.request(t, "/login", credentials("test-admin", "test-wrong-secret", loginPage.token(t)))
	wrong.check(t, http.StatusUnauthorized, "Неверный логин или пароль")
	if strings.Contains(wrong.Body, "test-wrong-secret") {
		t.Fatal("password returned in failed login HTML")
	}
	b.request(t, "/login", credentials("test-admin", adminPassword, wrong.token(t))).redirectTo(t, "/admin/users")
	admin := b.request(t, "/admin/users", nil)
	token := admin.token(t)
	if token == wrong.token(t) {
		t.Fatal("successful login reused the anonymous CSRF token")
	}
	b.request(t, "/", nil).check(t, http.StatusForbidden, "Нет доступа")

	invalid := b.request(t, "/admin/users", credentials("  ", studentPassword, token))
	invalid.check(t, http.StatusUnprocessableEntity, "")
	created := b.request(t, "/admin/users", credentials(" track-student ", studentPassword, token))
	if created.Status != http.StatusSeeOther || !strings.HasPrefix(created.Header.Get("Location"), "/admin/users") {
		t.Fatalf("create: HTTP %d Location=%q, want redirect to users", created.Status, created.Header.Get("Location"))
	}
	users := b.request(t, created.Header.Get("Location"), nil)
	users.check(t, http.StatusOK, "Ученик track-student создан")
	if !strings.Contains(users.Body, "<strong>track-student</strong>") || strings.Contains(users.Body, studentPassword) {
		t.Fatal("created student missing from table or password returned")
	}
	duplicate := b.request(t, "/admin/users", credentials("track-student", "test-duplicate-secret", token))
	duplicate.check(t, http.StatusConflict, "Логин track-student уже занят")
	if !strings.Contains(duplicate.Body, `value="track-student"`) || strings.Contains(duplicate.Body, "test-duplicate-secret") {
		t.Fatal("duplicate form did not preserve only login")
	}

	oldCookies := b.Jar.Cookies(mustURL(t, b.base))
	b.request(t, "/logout", url.Values{"csrf_token": {token}}).redirectTo(t, "/login")
	b.Jar.SetCookies(mustURL(t, b.base), oldCookies)
	b.request(t, "/admin/users", nil).redirectTo(t, "/login")

	studentToken := b.login(t, "track-student", studentPassword, "/")
	for _, path := range []string{"/admin/users", "/admin/log"} {
		p := b.request(t, path, nil)
		p.check(t, http.StatusForbidden, "Нет доступа")
		if strings.Contains(p.Body, "<table>") {
			t.Fatalf("admin table appeared on forbidden page %s", path)
		}
	}
	b.request(t, "/admin/users", credentials("another", studentPassword, studentToken)).
		check(t, http.StatusForbidden, "Нет доступа")
}

func mustURL(t *testing.T, value string) *url.URL {
	t.Helper()
	u, err := url.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestRejectsCrossSessionAndMissingCSRF(t *testing.T) {
	b, db := newBrowser(t, unusedAsker)
	login := b.request(t, "/login", nil)
	b.request(t, "/login?csrf_token="+login.token(t), credentials("test-admin", adminPassword, "")).
		check(t, http.StatusForbidden, "Нет доступа")
	attacker := b.request(t, "/login", nil).token(t)
	b.Jar, _ = cookiejar.New(nil)
	victim := b.request(t, "/login", nil).token(t)
	if attacker == victim {
		t.Fatal("different browsers received the same token")
	}
	b.request(t, "/login", credentials("test-admin", adminPassword, attacker)).
		check(t, http.StatusForbidden, "Нет доступа")
	admin := b.login(t, "test-admin", adminPassword, "/admin/users")
	b.request(t, "/admin/users", credentials("csrf-student", studentPassword, victim)).
		check(t, http.StatusForbidden, "Нет доступа")
	b.request(t, "/logout", url.Values{}).check(t, http.StatusForbidden, "Нет доступа")
	b.request(t, "/admin/users", nil).check(t, http.StatusOK, "Учеников пока нет")
	b.request(t, "/logout", url.Values{"csrf_token": {admin}}).redirectTo(t, "/login")
	if err := db.CreateStudent(t.Context(), "csrf-student", studentPassword); err != nil {
		t.Fatal(err)
	}
	b.login(t, "csrf-student", studentPassword, "/")
	b.request(t, "/ask", url.Values{"question": {"Надо ли уступить?"}}).
		check(t, http.StatusForbidden, "Нет доступа")
	queries, err := db.ListQueries(t.Context())
	if err != nil || len(queries) != 0 {
		t.Fatalf("invalid POST changed journal: %v, %v", queries, err)
	}
}

func TestQuestionValidationAndBodyLimit(t *testing.T) {
	b, db := newBrowser(t, unusedAsker)
	if err := db.CreateStudent(t.Context(), "test-student", studentPassword); err != nil {
		t.Fatal(err)
	}
	token := b.login(t, "test-student", studentPassword, "/")
	for _, question := range []string{" \t\n ", strings.Repeat("я", 2001)} {
		b.request(t, "/ask", url.Values{"question": {question}, "csrf_token": {token}}).
			check(t, http.StatusUnprocessableEntity, "")
	}
	b.request(t, "/ask", url.Values{"question": {strings.Repeat("x", 40000)}, "csrf_token": {token}}).
		check(t, http.StatusRequestEntityTooLarge, "")
	queries, err := db.ListQueries(t.Context())
	if err != nil || len(queries) != 0 {
		t.Fatalf("invalid question changed journal: %v, %v", queries, err)
	}
}

func TestAnswerStatesKeepSourcesAndWriteOneJournalEntry(t *testing.T) {
	hits := []search.Hit{{Document: search.Document{ID: "doc-test", Ref: "ПДД, п. 11.4", Text: "<script>document source</script>"}}}
	questions := []struct {
		question string
		result   rag.Result
		err      error
		status   int
		text     string
	}{
		{strings.Repeat("я", 2000), rag.Result{Answer: "Проверьте ПДД, п. 11.4", Sources: hits}, nil, http.StatusOK, "Проверьте ПДД, п. 11.4"},
		{"Неизвестный термин", rag.Result{Refused: true}, nil, http.StatusOK, "ничего не нашлось"},
		{"Ловушка", rag.Result{Answer: "Нет нужного пункта.", Sources: hits, Refused: true}, nil, http.StatusOK, "Недостаточно сведений"},
		{"Таймаут", rag.Result{Sources: hits}, fmt.Errorf("provider-private-detail: %w", context.DeadlineExceeded), http.StatusGatewayTimeout, "Ответ недоступен"},
		{"Ошибка", rag.Result{Sources: hits}, errors.New("provider-private-detail"), http.StatusBadGateway, "Ответ недоступен"},
	}
	b, db := newBrowser(t, func(_ context.Context, question string) (rag.Result, error) {
		for _, tt := range questions {
			if question == tt.question {
				return tt.result, tt.err
			}
		}
		return rag.Result{}, fmt.Errorf("unexpected question")
	})
	if err := db.CreateStudent(t.Context(), "test-student", studentPassword); err != nil {
		t.Fatal(err)
	}
	token := b.login(t, "test-student", studentPassword, "/")
	for _, tt := range questions {
		p := b.request(t, "/ask", url.Values{"question": {"  " + tt.question + "  "}, "csrf_token": {token}})
		p.check(t, tt.status, tt.text)
		if strings.Contains(p.Body, "doc-test") != (len(tt.result.Sources) > 0) {
			t.Fatalf("answer sources did not match RAG output for %q", tt.text)
		}
		if strings.Contains(p.Body, "<script>document source") || strings.Contains(p.Body, "provider-private-detail") {
			t.Fatal("answer exposed raw HTML or provider details")
		}
	}
	queries, err := db.ListQueries(t.Context())
	if err != nil || len(queries) != len(questions) {
		t.Fatalf("stored %d queries, want %d (%v)", len(queries), len(questions), err)
	}
	for i, query := range queries {
		if query.Login != "test-student" || query.Question != questions[len(questions)-1-i].question {
			t.Fatalf("journal entry %d did not preserve student, question, or order", i)
		}
	}
	b.request(t, "/logout", url.Values{"csrf_token": {token}}).redirectTo(t, "/login")
	b.login(t, "test-admin", adminPassword, "/admin/users")
	b.request(t, "/admin/log", nil).check(t, http.StatusOK, "Таймаут")
}

func TestQuestionReportsJournalFailure(t *testing.T) {
	b, db := newBrowser(t, unusedAsker)
	if err := db.CreateStudent(t.Context(), "test-student", studentPassword); err != nil {
		t.Fatal(err)
	}
	token := b.login(t, "test-student", studentPassword, "/")
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	p := b.request(t, "/ask", url.Values{"question": {"Как уступить?"}, "csrf_token": {token}})
	p.check(t, http.StatusInternalServerError, "")
	if strings.Contains(p.Body, "database is closed") {
		t.Fatal("internal database error returned in HTML")
	}
}
