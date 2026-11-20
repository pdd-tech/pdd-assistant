// Package auth manages browser sessions and their CSRF tokens in memory.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"net/http"
	"sync"
	"time"

	"github.com/pdd-tech/pdd-assistant/internal/store"
)

const (
	cookieName = "pdd_session"
	guestTTL   = 30 * time.Minute
	userTTL    = 8 * time.Hour
)

type Session struct {
	User      store.User
	CSRFToken string
	expiresAt time.Time
}

type Sessions struct {
	mu        sync.Mutex
	entries   map[string]Session
	nextSweep time.Time
	secure    bool
}

func New(secure bool) *Sessions {
	return &Sessions{entries: make(map[string]Session), secure: secure}
}

func (s *Sessions) Get(r *http.Request) (Session, bool) {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return Session{}, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.entries[cookie.Value]
	if !ok {
		return Session{}, false
	}
	if !time.Now().Before(session.expiresAt) {
		delete(s.entries, cookie.Value)
		return Session{}, false
	}
	return session, true
}

func (s *Sessions) Start(w http.ResponseWriter, r *http.Request, user store.User) Session {
	now := time.Now()
	ttl := guestTTL
	if user.ID != 0 {
		ttl = userTTL
	}
	id := rand.Text()
	session := Session{User: user, CSRFToken: rand.Text(), expiresAt: now.Add(ttl)}

	s.mu.Lock()
	s.sweepExpired(now)
	if old, err := r.Cookie(cookieName); err == nil {
		delete(s.entries, old.Value)
	}
	s.entries[id] = session
	s.mu.Unlock()

	s.setCookie(w, id, int(ttl/time.Second), session.expiresAt)
	return session
}

func (s *Sessions) End(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(cookieName); err == nil {
		s.mu.Lock()
		delete(s.entries, cookie.Value)
		s.mu.Unlock()
	}
	s.setCookie(w, "", -1, time.Unix(1, 0))
}

func ValidCSRF(session Session, token string) bool {
	return session.CSRFToken != "" &&
		subtle.ConstantTimeCompare([]byte(session.CSRFToken), []byte(token)) == 1
}

// Start prunes old sessions at most once a minute without a background goroutine.
func (s *Sessions) sweepExpired(now time.Time) {
	if now.Before(s.nextSweep) {
		return
	}
	for id, session := range s.entries {
		if !now.Before(session.expiresAt) {
			delete(s.entries, id)
		}
	}
	s.nextSweep = now.Add(time.Minute)
}

func (s *Sessions) setCookie(w http.ResponseWriter, id string, maxAge int, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    id,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
	})
}
