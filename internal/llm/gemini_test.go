package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pdd-tech/pdd-assistant/internal/search"
)

func TestGeminiSchemaAndModelIdentity(t *testing.T) {
	for _, actual := range []string{"gemini-3.8-flash", "models/gemini-3.8-flash", "gemini-3.5-flash-lite"} {
		t.Run(actual, func(t *testing.T) {
			c, err := New(Config{BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai", Model: "gemini-3.8-flash", APIKey: "google-test-key", Timeout: time.Second, JSONMode: true, ReasoningEffort: "low"})
			if err != nil {
				t.Fatal(err)
			}
			c.httpClient.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions" || r.Header.Get("Authorization") != "Bearer google-test-key" {
					t.Error("wrong endpoint or auth")
				}
				var request chatRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Fatal(err)
				}
				if request.ReasoningEffort != "low" || request.ResponseFormat == nil || request.ResponseFormat.Type != "json_schema" || request.ResponseFormat.JSONSchema == nil {
					t.Error("Gemini schema or reasoning absent")
				}
				data, _ := json.Marshal(request.ResponseFormat)
				for _, field := range []string{`"quotes"`, `"source"`, `"enum":["a"]`, `"strict":true`} {
					if !strings.Contains(string(data), field) {
						t.Errorf("schema missing %s", field)
					}
				}
				content := `{"refused":false,"parts":[{"text":"Проезд разрешён.","source":"a","quotes":["Проезд разрешён."]}],"missing":""}`
				body, _ := json.Marshal(map[string]any{"model": actual, "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": content}}}})
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
			})
			got, err := c.Complete(context.Background(), "Можно проехать?", []search.Document{{ID: "a", Ref: "Правило А", Text: "Проезд разрешён."}})
			if actual == "gemini-3.5-flash-lite" {
				if err == nil || got != (Response{}) {
					t.Error("silent model replacement accepted")
				}
			} else if err != nil || got.Answer != "Проезд разрешён (Правило А)." {
				t.Fatalf("got=%+v err=%v", got, err)
			}
		})
	}
}

func TestGeminiRejectsWrongProviderKeyAndUnsupportedThinking(t *testing.T) {
	for _, cfg := range []Config{
		{BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai", Model: "gemini-3.8-flash", Timeout: time.Second},
		{BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai", Model: "gemini-3.8-flash", APIKey: "sk-or-test", Timeout: time.Second},
		{BaseURL: "http://generativelanguage.googleapis.com/v1beta/openai", Model: "gemini-3.8-flash", APIKey: "test", Timeout: time.Second},
		{BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai", Model: "gemini-3.8-flash", APIKey: "test", Timeout: time.Second, DisableReasoning: true},
	} {
		if _, err := New(cfg); err == nil {
			t.Error("invalid Google profile accepted")
		}
	}
}

func TestGoogleModelListUsesOnlyItsOwnKey(t *testing.T) {
	c, err := New(Config{BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai", Model: "gemini-3.8-flash", APIKey: "google-test-key", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	c.httpClient.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1beta/openai/models" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer google-test-key" {
			t.Error("key routed incorrectly")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"models/gemini-3.8-flash"}]}`)), Header: make(http.Header)}, nil
	})
	if _, err := c.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
}
