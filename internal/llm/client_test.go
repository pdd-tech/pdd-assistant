package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pdd-tech/pdd-assistant/internal/search"
)

func TestCompleteWithProviderWithoutStructuredOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer example-key" {
			t.Error("Bearer authorization missing")
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type: %q", got)
		}

		var payload struct {
			Model           string          `json:"model"`
			ReasoningEffort string          `json:"reasoning_effort"`
			ResponseFormat  json.RawMessage `json:"response_format"`
			Messages        []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("request JSON: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if payload.ResponseFormat != nil {
			http.Error(w, "structured output is unavailable", http.StatusNotImplemented)
			return
		}
		if payload.Model != "local-model" || payload.ReasoningEffort != "none" {
			t.Errorf("generation settings: %+v", payload)
		}
		if len(payload.Messages) != 2 || payload.Messages[0].Role != "system" || payload.Messages[1].Role != "user" {
			t.Errorf("expected separate system and user messages: %+v", payload.Messages)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var input struct {
			Question  string `json:"question"`
			Documents []struct {
				ID   string `json:"id"`
				Ref  string `json:"ref"`
				Text string `json:"text"`
			} `json:"documents"`
		}
		if err := json.Unmarshal([]byte(payload.Messages[1].Content), &input); err != nil {
			t.Errorf("prompt data JSON: %v", err)
		}
		if input.Question != "Где это записано?" || len(input.Documents) != 1 || input.Documents[0].ID != "pdd-11.4" || input.Documents[0].Ref != "ПДД, п. 11.4" || input.Documents[0].Text != "Обгон запрещён на мостах." {
			t.Errorf("unexpected model input: %+v", input)
		}
		writeCompletion(w, "stop", `{"refused":false,"parts":[{"text":"Обгон на мостах запрещён.","source":"pdd-11.4"}],"missing":""}`)
	}))
	defer server.Close()

	client, err := New(Config{
		BaseURL: server.URL + "/v1/", APIKey: "example-key", Model: "local-model",
		Timeout: time.Second, DisableReasoning: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Complete(context.Background(), "Где это записано?", []search.Document{
		{ID: "pdd-11.4", Ref: "ПДД, п. 11.4", Text: "Обгон запрещён на мостах."},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := Response{Answer: "Обгон на мостах запрещён (ПДД, п. 11.4)."}
	if result != want {
		t.Errorf("result = %+v, want %+v", result, want)
	}
}

func TestCompleteAcceptsExplicitRefusalWithoutKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("empty API key sent as authorization")
		}
		var payload map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("request JSON: %v", err)
		}
		if _, ok := payload["reasoning_effort"]; ok {
			t.Error("reasoning_effort sent without DisableReasoning")
		}
		writeCompletion(w, "stop", `{"refused":true,"parts":[],"missing":"не указан срок действия пропуска."}`)
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL, Model: "local-model", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Complete(context.Background(), "Вопрос без данных", nil)
	if err != nil || !result.Refused || result.Answer != "По найденным документам полный ответ невозможен. Не хватает сведений: не указан срок действия пропуска." {
		t.Fatalf("expected explicit refusal, got %+v, %v", result, err)
	}
}

func TestCompleteRejectsInvalidResponses(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		response    string
		content     string
		finish      string
		errorDetail string
	}{
		{name: "429", status: 429, response: "provider secret", errorDetail: "429"},
		{name: "503", status: 503, response: "provider secret", errorDetail: "503"},
		{name: "invalid envelope", response: "provider secret"},
		{name: "missing choices", response: `{"choices":[]}`},
		{name: "oversized", response: strings.Repeat("x", 1<<20)},
		{name: "invalid content", content: "provider secret"},
		{name: "missing refused", content: `{"answer":"provider secret"}`},
		{name: "null refused", content: `{"answer":"provider secret","refused":null}`},
		{name: "string refused", content: `{"answer":"provider secret","refused":"false"}`},
		{name: "blank answer", content: `{"refused":false,"parts":[],"missing":""}`},
		{name: "unknown source", content: `{"refused":false,"parts":[{"text":"Вывод","source":"unseen"}],"missing":""}`},
		{name: "blank part", content: `{"refused":false,"parts":[{"text":" ","source":"source"}],"missing":""}`},
		{name: "refusal without explanation", content: `{"refused":true,"parts":[],"missing":""}`},
		{name: "truncated", finish: "length", content: `{"answer":"provider secret","refused":false}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.status != 0 {
					w.WriteHeader(tt.status)
				}
				if tt.response != "" {
					fmt.Fprint(w, tt.response)
					return
				}
				writeCompletion(w, tt.finish, tt.content)
			}))
			defer server.Close()

			client, err := New(Config{BaseURL: server.URL, Model: "local-model", Timeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			result, err := client.Complete(context.Background(), "?", []search.Document{
				{ID: "source", Ref: "Правило 1", Text: "Проезд разрешён."},
			})
			if result != (Response{}) || err == nil {
				t.Fatalf("result = %+v, error = %v, want response rejected", result, err)
			}
			if !strings.Contains(err.Error(), tt.errorDetail) || strings.Contains(err.Error(), "provider secret") {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestRefuseWhenModelReportsMissingContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeCompletion(w, "stop", `{"refused":false,"parts":[{"text":"Неподтверждённый вывод","source":"a"}],"missing":"срок действия"}`)
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL, Model: "local-model", Timeout: time.Second, DisableReasoning: true})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Complete(context.Background(), "Каков срок?", nil)
	if err != nil || !result.Refused || result.Answer != "По найденным документам полный ответ невозможен. Не хватает сведений: срок действия" {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}

func TestCompleteRetriesInvalidReasoningOutputOnce(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := requests.Add(1)
		var payload struct {
			ReasoningEffort string `json:"reasoning_effort"`
			MaxTokens       int    `json:"max_tokens"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if attempt == 1 {
			if payload.ReasoningEffort != "" {
				t.Error("reasoning disabled on the first request")
			}
			writeCompletion(w, "length", "")
			return
		}
		if payload.ReasoningEffort != "none" || payload.MaxTokens != 2048 {
			t.Errorf("unexpected fallback settings: %+v", payload)
		}
		writeCompletion(w, "stop", `{"refused":false,"parts":[{"text":"Остановитесь.","source":"a"}],"missing":""}`)
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL, Model: "local-model", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Complete(context.Background(), "Что делать?", []search.Document{
		{ID: "a", Ref: "Правило 1", Text: "Остановитесь."},
	})
	if err != nil || result.Answer != "Остановитесь (Правило 1)." || requests.Load() != 2 {
		t.Fatalf("result=%+v error=%v requests=%d", result, err, requests.Load())
	}
}

func TestCompleteTimeoutIncludesReadingBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL, Model: "local-model", Timeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Complete(context.Background(), "?", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
}

func TestNewRejectsInvalidConfiguration(t *testing.T) {
	for _, cfg := range []Config{
		{BaseURL: "file:///tmp/pdd", Model: "test", Timeout: time.Second},
		{BaseURL: "http://user:secret@localhost", Model: "test", Timeout: time.Second},
		{BaseURL: "https://example.invalid?key=secret", Model: "test", Timeout: time.Second},
		{BaseURL: "http://localhost", Timeout: time.Second},
		{BaseURL: "http://localhost", Model: "test"},
	} {
		if _, err := New(cfg); err == nil || strings.Contains(err.Error(), "secret") {
			t.Errorf("invalid config accepted or exposed: %v", err)
		}
	}
}

func writeCompletion(w http.ResponseWriter, finish, content string) {
	msg := map[string]string{"role": "assistant", "content": content}
	choice := map[string]any{"index": 0, "finish_reason": finish, "message": msg}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"choices": []any{choice}})
}
