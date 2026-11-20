package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRAGRunAndScore(t *testing.T) {
	base := t.TempDir()
	docs := testDocs()
	for i := range docs {
		docs[i].Text = "обгон моста"
	}
	queries := testQueries()
	for i := range queries {
		queries[i].Query = "обгон"
		queries[i].ReferenceAnswer = "эталон только для проверяющего"
	}
	queries[len(queries)-1].Query = "zzznoresults"
	writeJSON := func(name string, data any) string {
		t.Helper()
		path := filepath.Join(base, name)
		content, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	writeJSON("koap.json", docs[:10])
	writeJSON("pdd.json", docs[10:])
	queryPath := writeJSON("queries.json", queries)

	for _, unavailable := range []bool{false, true} {
		name := "completed"
		if unavailable {
			name = "failed"
		}
		t.Run(name, func(t *testing.T) {
			var requests atomic.Int32
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				var req struct {
					Messages []struct{ Content string }
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
				}
				for _, message := range req.Messages {
					if strings.Contains(message.Content, "эталон только для проверяющего") {
						t.Error("benchmark reference reached the model")
					}
				}
				if unavailable {
					http.Error(w, "private provider error", http.StatusTooManyRequests)
					return
				}
				result := `{"refused":false,"parts":[{"text":"Пример ответа","source":"d0"}],"missing":""}`
				_ = json.NewEncoder(w).Encode(map[string]any{
					"choices": []any{map[string]any{
						"finish_reason": "stop",
						"message":       map[string]string{"content": result},
					}},
				})
			}))
			defer model.Close()
			t.Setenv("LLM_BASE_URL", model.URL)
			t.Setenv("LLM_MODEL", "test-model")
			runPath := filepath.Join(base, name+".json")
			err := run([]string{"-mode", "rag", "-corpus", base, "-queries", queryPath, "-out", runPath})
			if (err != nil) != unavailable {
				t.Fatalf("run returned %v", err)
			}
			data, err := os.ReadFile(runPath)
			if err != nil {
				t.Fatal(err)
			}
			var got Run
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			if got.Status != name || got.Model != "test-model" || len(got.Results) != 15 || requests.Load() != 14 {
				t.Fatalf("status=%q model=%q results=%d calls=%d", got.Status, got.Model, len(got.Results), requests.Load())
			}
			if len(got.Results[0].Hits) != 30 || len(got.Results[0].Sources) != 5 || got.ConfigSHA256 == "" {
				t.Fatal("full rankings, model sources or configuration hash are missing")
			}
			if !got.Results[14].Refused || got.Results[14].Answer == "" {
				t.Fatal("empty retrieval did not produce a refusal")
			}
			if unavailable && (got.Results[0].Refused || got.Results[0].Error == "" || strings.Contains(string(data), "private provider error")) {
				t.Fatal("provider failure mislabeled or provider details leaked")
			}
			report, err := score(runPath, "", queryPath)
			if (err != nil) != unavailable || !unavailable && !strings.Contains(report, "| Accuracy | не оценено") {
				t.Fatalf("unexpected score result: %v %s", err, report)
			}
			if !unavailable {
				got.Results[0].Answer = ""
				invalidRun := writeJSON("no-answer.json", got)
				if _, err := score(invalidRun, "", queryPath); err == nil {
					t.Fatal("score accepted completed RAG run with one missing answer")
				}
			}
		})
	}
}
