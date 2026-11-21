package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadJSONLAndRejectDuplicateBeforeNetwork(t *testing.T) {
	d := t.TempDir()
	corpus := filepath.Join(d, "corpus.jsonl")
	queries := filepath.Join(d, "questions.jsonl")
	line := `{"id":"RU1","title":"Полив","text":"Датчик влажности включает воду."}` + "\n"
	if err := os.WriteFile(corpus, []byte(line), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(queries, []byte(`{"id":"Q01","question":"Автоматический полив?"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	docs, qs, csha, qsha, err := load(corpus, queries)
	if err != nil || len(docs) != 1 || len(qs) != 1 || docs[0].Text != "Полив\nДатчик влажности включает воду." || csha == "" || qsha == "" {
		t.Fatalf("invalid load: %v", err)
	}
	if err := os.WriteFile(corpus, []byte(line+line), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := load(corpus, queries); err == nil || !strings.Contains(err.Error(), "дубликат") {
		t.Error("duplicate input accepted")
	}
}
