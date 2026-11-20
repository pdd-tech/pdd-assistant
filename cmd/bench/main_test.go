package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pdd-tech/pdd-assistant/internal/search"
)

func TestMetrics(t *testing.T) {
	rel := []string{"A", "D", "F"}
	top := []string{"B", "A", "D"}
	if got := recallAt(rel, top, 3); math.Abs(got-2.0/3) > 1e-12 {
		t.Fatalf("Recall@3=%g", got)
	}
	if got := reciprocalRank(rel, top, 30); got != .5 {
		t.Fatalf("RR=%g", got)
	}
	if reciprocalRank(rel, top, 1) != 0 || reciprocalRank(rel, []string{"X"}, 30) != 0 {
		t.Fatal("RR вне глубины или без relevant должен быть 0")
	}
}

func testDocs() []search.Document {
	var docs []search.Document
	for i := 0; i < 30; i++ {
		docs = append(docs, search.Document{ID: fmt.Sprint("d", i), Ref: "r", Text: "t"})
	}
	return docs
}

func testQueries() []Query {
	var qs []Query
	for i := 0; i < 10; i++ {
		qs = append(qs, Query{ID: fmt.Sprint("q", i), Kind: "normal", Query: "q", Relevant: []string{"d1", "d2", "d3"}, ReferenceAnswer: "a"})
	}
	for i := 0; i < 5; i++ {
		qs = append(qs, Query{ID: fmt.Sprint("t", i), Kind: "trap", Query: "q", ReferenceAnswer: "нет"})
	}
	return qs
}

func TestValidate(t *testing.T) {
	if err := validate(testDocs(), testQueries()); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func([]Query) []Query{
		"неизвестный relevant": func(q []Query) []Query { q[0].Relevant = []string{"d1", "d2", "zzz"}; return q },
		"два relevant":         func(q []Query) []Query { q[0].Relevant = []string{"d1", "d2"}; return q },
		"повтор relevant":      func(q []Query) []Query { q[0].Relevant = []string{"d1", "d2", "d2"}; return q },
		"дубликат id":          func(q []Query) []Query { q[1].ID = q[0].ID; return q },
		"пустой эталон":        func(q []Query) []Query { q[0].ReferenceAnswer = " "; return q },
		"trap с relevant":      func(q []Query) []Query { q[10].Relevant = []string{"d1"}; return q },
		"состав":               func(q []Query) []Query { q[0].Kind = "trap"; q[0].Relevant = nil; return q },
		"15 вопросов":          func(q []Query) []Query { return q[:14] },
	}
	for name, mutate := range cases {
		if validate(testDocs(), mutate(testQueries())) == nil {
			t.Errorf("%s: набор принят", name)
		}
	}
	if validate(testDocs()[:29], testQueries()) == nil {
		t.Error("29 документов приняты")
	}
}

func TestRealDataset(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "retrieval.json")
	report := filepath.Join(dir, "results.md")
	if err := run([]string{"-mode", "retrieval", "-corpus", "../../corpus", "-queries", "../../benchmark/queries.json", "-out", out}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"-mode", "score", "-run", out, "-queries", "../../benchmark/queries.json", "-out", report}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(report)
	if !strings.Contains(string(data), "| Accuracy | не оценено") {
		t.Fatalf("Accuracy без оценок должна быть «не оценено»:\n%s", data)
	}
}

func TestGradesRejected(t *testing.T) {
	dir := t.TempDir()
	qpath := filepath.Join(dir, "queries.json")
	qdata, _ := json.Marshal(testQueries())
	os.WriteFile(qpath, qdata, 0o644)
	r := Run{Mode: "rag", RunID: "r1", QueriesSHA256: sum(qdata), K: 5, MRRDepth: 30}
	var grades []Grade
	for _, q := range testQueries() {
		r.Results = append(r.Results, Result{ID: q.ID, Hits: []search.Hit{{Document: search.Document{ID: "d1"}}}})
		grades = append(grades, Grade{ID: q.ID, Correct: true})
	}
	rdata, _ := json.Marshal(r)
	rpath := filepath.Join(dir, "run.json")
	os.WriteFile(rpath, rdata, 0o644)
	try := func(g Grades) error {
		gdata, _ := json.Marshal(g)
		gpath := filepath.Join(dir, "grades.json")
		os.WriteFile(gpath, gdata, 0o644)
		_, err := score(rpath, gpath, qpath)
		return err
	}
	if err := try(Grades{"r1", sum(rdata), grades}); err != nil {
		t.Fatal(err)
	}
	dup := append(append([]Grade{}, grades[:14]...), grades[0])
	for name, g := range map[string]Grades{
		"хеш":         {"r1", "bad", grades},
		"run_id":      {"r2", sum(rdata), grades},
		"пропуск":     {"r1", sum(rdata), grades[:14]},
		"дубликат":    {"r1", sum(rdata), dup},
		"неизвестный": {"r1", sum(rdata), append(append([]Grade{}, grades[:14]...), Grade{ID: "x"})},
	} {
		if try(g) == nil {
			t.Errorf("%s: оценки приняты", name)
		}
	}
}
