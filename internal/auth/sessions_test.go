package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pdd-tech/pdd-assistant/internal/store"
)

func newRequest(cookie *http.Cookie) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	return req
}

func responseCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one cookie, got %d", len(cookies))
	}
	return cookies[0]
}

func TestLoginRotatesSessionAndCSRFToken(t *testing.T) {
	sessions := New(false)
	rec := httptest.NewRecorder()
	anonymous := sessions.Start(rec, newRequest(nil), store.User{})
	old := responseCookie(t, rec)
	if !old.HttpOnly || old.Secure || old.SameSite != http.SameSiteLaxMode ||
		old.Path != "/" || old.MaxAge != 1800 {
		t.Fatalf("anonymous session cookie flags: %+v", old)
	}
	if _, ok := sessions.Get(newRequest(old)); !ok {
		t.Fatal("anonymous session was not registered")
	}
	if !ValidCSRF(anonymous, anonymous.CSRFToken) || ValidCSRF(anonymous, "") {
		t.Fatal("anonymous session does not verify CSRF")
	}

	rec = httptest.NewRecorder()
	user := store.User{ID: 1, Login: "student", Role: store.RoleStudent}
	loggedIn := sessions.Start(rec, newRequest(old), user)
	cookie := responseCookie(t, rec)
	if old.Value == cookie.Value || cookie.MaxAge != 28800 {
		t.Fatal("authenticated cookie was not rotated with the full lifetime")
	}
	if _, ok := sessions.Get(newRequest(old)); ok {
		t.Fatal("anonymous session survived login")
	}
	if session, ok := sessions.Get(newRequest(cookie)); !ok || session.User != user {
		t.Fatalf("authenticated session unavailable: %v", ok)
	}
	if ValidCSRF(loggedIn, anonymous.CSRFToken) || !ValidCSRF(loggedIn, loggedIn.CSRFToken) {
		t.Fatal("CSRF token was not rotated")
	}
}

func TestLogoutDeletesSecureSession(t *testing.T) {
	sessions := New(true)
	rec := httptest.NewRecorder()
	sessions.Start(rec, newRequest(nil), store.User{ID: 1})
	cookie := responseCookie(t, rec)
	if !cookie.Secure {
		t.Fatal("missing Secure flag")
	}
	req := newRequest(cookie)
	rec = httptest.NewRecorder()
	sessions.End(rec, req)
	removed := responseCookie(t, rec)
	if removed.MaxAge >= 0 || !removed.Secure || removed.Path != cookie.Path {
		t.Fatal("session cookie wasn't cleared")
	}
	if _, ok := sessions.Get(req); ok {
		t.Fatal("session survived logout")
	}
}

func TestExpiredSessionIsRejected(t *testing.T) {
	sessions := New(false)
	rec := httptest.NewRecorder()
	sessions.Start(rec, newRequest(nil), store.User{ID: 1})
	cookie := responseCookie(t, rec)
	session := sessions.entries[cookie.Value]
	session.expiresAt = time.Now().Add(-time.Second)
	sessions.entries[cookie.Value] = session
	if _, ok := sessions.Get(newRequest(cookie)); ok {
		t.Fatal("expired session is valid")
	}
}

func TestZeroSessionDoesNotVerifyEmptyCSRF(t *testing.T) {
	if ValidCSRF(Session{}, "") {
		t.Fatal("empty session verified an empty token")
	}
}
