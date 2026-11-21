package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pdd-tech/pdd-assistant/internal/search"
)

func TestGigaOAuthCacheRefreshAndSchema(t *testing.T) {
	c, err := New(Config{BaseURL: "https://api.giga.chat/v1", Model: "GigaChat-3-Ultra", GigaCredentials: "test-auth-key", Timeout: time.Second, JSONMode: true})
	if err != nil {
		t.Fatal(err)
	}
	var oauth atomic.Int32
	c.httpClient.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		var body string
		if r.URL.String() == gigaTokenURL {
			oauth.Add(1)
			if r.Header.Get("Authorization") != "Basic test-auth-key" || !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(r.Header.Get("RqUID")) {
				t.Error("invalid OAuth auth or RqUID")
			}
			data, _ := io.ReadAll(r.Body)
			if string(data) != "scope=GIGACHAT_API_PERS" {
				t.Error("wrong scope")
			}
			payload, _ := json.Marshal(map[string]any{"access_token": "access-test", "expires_at": time.Now().Add(time.Hour).UnixMilli()})
			body = string(payload)
		} else {
			if r.Header.Get("Authorization") != "Bearer access-test" {
				t.Error("Authorization key used as access token")
			}
			var req chatRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			if req.ResponseFormat == nil || req.ResponseFormat.Schema == nil || req.ResponseFormat.JSONSchema != nil || !req.ResponseFormat.Strict || req.ReasoningEffort != "" {
				t.Error("wrong GigaChat schema format")
			}
			body = `{"choices":[{"finish_reason":"stop","message":{"content":"{\"refused\":false,\"parts\":[{\"text\":\"Проезд разрешён.\",\"source\":\"a\",\"quotes\":[\"Проезд разрешён.\"]}],\"missing\":\"\"}"}}]}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.bearer(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if oauth.Load() != 1 {
		t.Fatal("concurrent requests did not share the token")
	}
	c.giga.expires = time.Now().Add(-time.Second)
	got, err := c.Complete(context.Background(), "Можно проехать?", []search.Document{{ID: "a", Ref: "Правило А", Text: "Проезд разрешён."}})
	if err != nil || !strings.Contains(got.Answer, "Правило А") || oauth.Load() != 2 {
		t.Fatalf("got=%+v err=%v oauth=%d", got, err, oauth.Load())
	}
	settings, _ := json.Marshal(c.Settings())
	if strings.Contains(string(settings), "test-auth-key") || strings.Contains(string(settings), "access-test") {
		t.Fatal("token or key leaked to settings")
	}
}

func TestGigaOAuthFailureAndCancelledRefresh(t *testing.T) {
	c, err := New(Config{BaseURL: "https://api.giga.chat/v1", Model: "GigaChat-3-Ultra", GigaCredentials: "private-key", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	c.httpClient.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader("private response")), Header: make(http.Header)}, nil
	})
	if _, err := c.bearer(context.Background()); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatal("OAuth failure leaked or was ignored")
	}
	c.giga.gate <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err = c.bearer(ctx)
	<-c.giga.gate
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("refresh wait ignored context: %v", err)
	}
}
