package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pdd-tech/pdd-assistant/internal/search"
)

func TestPatentProfileUsesReferencesWithoutQuoteGate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(req.Messages[0].Content, "ПДД") || !strings.Contains(req.Messages[0].Content, "аналог") {
			t.Error("wrong domain prompt")
		}
		writeCompletion(w, "stop", `{"refused":false,"parts":[{"text":"Частичный аналог: используется датчик влажности; удалённое управление не описано.","source":"RU1"}],"missing":""}`)
	}))
	defer server.Close()
	c, err := New(Config{BaseURL: server.URL, Model: "test", Timeout: time.Second, Profile: "patents"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.Complete(context.Background(), "Полив по датчику с удалённым управлением", []search.Document{{ID: "RU1", Ref: "Патент RU1", Text: "Автоматический полив по датчику влажности."}})
	if err != nil || got.Refused || !strings.Contains(got.Answer, "Патент RU1") {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if c.Settings().RequireQuotes || c.Settings().Profile != "patents" {
		t.Error("wrong evidence settings")
	}
	f := answerFormat([]search.Document{{ID: "RU1"}}, true, false)
	data, _ := json.Marshal(f)
	if strings.Contains(string(data), "quotes") || !strings.Contains(string(data), `"enum":["RU1"]`) {
		t.Error("patent schema must enforce actual source IDs without requiring retyped quotes")
	}
}
