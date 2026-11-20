// Command bench запускает retrieval и считает метрики benchmark КТ2 (см. spec/tech/rag.md).
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pdd-tech/pdd-assistant/internal/search"
)

type Query struct {
	ID              string   `json:"id"`
	Kind            string   `json:"kind"`
	Theme           string   `json:"theme"`
	Query           string   `json:"query"`
	Relevant        []string `json:"relevant"`
	ReferenceAnswer string   `json:"reference_answer"`
}

type Result struct {
	ID        string       `json:"id"`
	Hits      []search.Hit `json:"hits"`
	ElapsedMS float64      `json:"elapsed_ms"`
	Answer    string       `json:"answer,omitempty"`
	Refused   bool         `json:"refused,omitempty"`
	Error     string       `json:"error,omitempty"`
}

type Run struct {
	Mode          string   `json:"mode"`
	RunID         string   `json:"run_id"`
	StartedAt     string   `json:"started_at"`
	CorpusSHA256  string   `json:"corpus_sha256"`
	QueriesSHA256 string   `json:"queries_sha256"`
	ConfigSHA256  string   `json:"config_sha256"`
	K             int      `json:"k"`
	K1            float64  `json:"k1"`
	B             float64  `json:"b"`
	MRRDepth      int      `json:"mrr_depth"`
	Model         string   `json:"model,omitempty"`
	Results       []Result `json:"results"`
}

type Grade struct {
	ID      string `json:"id"`
	Correct bool   `json:"correct"`
	Comment string `json:"comment"`
}

type Grades struct {
	RunID     string  `json:"run_id"`
	RunSHA256 string  `json:"run_sha256"`
	Grades    []Grade `json:"grades"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ошибка:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("bench", flag.ContinueOnError)
	mode := fs.String("mode", "", "retrieval | rag | score")
	out := fs.String("out", "", "файл результата (по умолчанию stdout)")
	corpusDir := fs.String("corpus", "corpus", "папка корпуса")
	queriesPath := fs.String("queries", "benchmark/queries.json", "файл вопросов")
	runPath := fs.String("run", "", "score: файл прогона")
	gradesPath := fs.String("grades", "", "score: файл оценок (необязателен)")
	k := fs.Int("k", 5, "сколько документов идёт в контекст")
	k1 := fs.Float64("k1", 1.2, "параметр BM25 k1")
	b := fs.Float64("b", 0.75, "параметр BM25 b")
	if err := fs.Parse(args); err != nil {
		return err
	}
	switch *mode {
	case "retrieval":
		r, err := retrieval(*corpusDir, *queriesPath, *k, *k1, *b)
		if err != nil {
			return err
		}
		data, _ := json.MarshalIndent(r, "", "  ")
		return write(*out, append(data, '\n'))
	case "score":
		report, err := score(*runPath, *gradesPath, *queriesPath)
		if err != nil {
			return err
		}
		return write(*out, []byte(report))
	case "rag":
		return fmt.Errorf("режим rag подключается при интеграции")
	}
	return fmt.Errorf("укажите -mode retrieval, rag или score")
}

func write(path string, data []byte) error {
	if path == "" {
		_, err := os.Stdout.Write(data)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func loadQueries(path string, docs []search.Document) ([]Query, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var qs []Query
	if err := json.Unmarshal(data, &qs); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}
	return qs, data, validate(docs, qs)
}

// validate проверяет состав набора до запуска: 30 документов, 10 normal + 5 trap, разметка.
func validate(docs []search.Document, qs []Query) error {
	if len(docs) != 30 {
		return fmt.Errorf("в корпусе %d документов, ожидается 30", len(docs))
	}
	known := map[string]bool{}
	for _, d := range docs {
		known[d.ID] = true
	}
	seen := map[string]bool{}
	normal, trap := 0, 0
	for _, q := range qs {
		if q.ID == "" || strings.TrimSpace(q.Query) == "" || strings.TrimSpace(q.ReferenceAnswer) == "" {
			return fmt.Errorf("вопрос %q: требуются id, query и reference_answer", q.ID)
		}
		if seen[q.ID] {
			return fmt.Errorf("дубликат вопроса %q", q.ID)
		}
		seen[q.ID] = true
		switch q.Kind {
		case "normal":
			normal++
			rel := map[string]bool{}
			for _, id := range q.Relevant {
				if !known[id] {
					return fmt.Errorf("вопрос %s: relevant %q нет в корпусе", q.ID, id)
				}
				rel[id] = true
			}
			if len(rel) < 3 || len(rel) != len(q.Relevant) {
				return fmt.Errorf("вопрос %s: нужно минимум 3 различных relevant", q.ID)
			}
		case "trap":
			trap++
			if len(q.Relevant) != 0 {
				return fmt.Errorf("вопрос %s: у trap relevant должен быть пустым", q.ID)
			}
		default:
			return fmt.Errorf("вопрос %s: kind %q, ожидается normal или trap", q.ID, q.Kind)
		}
	}
	if normal != 10 || trap != 5 {
		return fmt.Errorf("состав набора %d normal + %d trap, ожидается 10 + 5", normal, trap)
	}
	return nil
}

func retrieval(corpusDir, queriesPath string, k int, k1, b float64) (*Run, error) {
	docs, err := search.LoadCorpus(corpusDir)
	if err != nil {
		return nil, err
	}
	qs, qdata, err := loadQueries(queriesPath, docs)
	if err != nil {
		return nil, err
	}
	idx, err := search.New(docs, k1, b)
	if err != nil {
		return nil, err
	}
	depth := len(docs)
	corpusJSON, _ := json.Marshal(docs)
	cfg, _ := json.Marshal(map[string]any{"k": k, "k1": k1, "b": b, "mrr_depth": depth})
	now := time.Now().UTC()
	r := &Run{Mode: "retrieval", RunID: now.Format("20060102T150405Z") + "-retrieval", StartedAt: now.Format(time.RFC3339),
		CorpusSHA256: sum(corpusJSON), QueriesSHA256: sum(qdata), ConfigSHA256: sum(cfg), K: k, K1: k1, B: b, MRRDepth: depth}
	for _, q := range qs {
		start := time.Now()
		hits := idx.Search(q.Query, depth)
		r.Results = append(r.Results, Result{ID: q.ID, Hits: hits, ElapsedMS: float64(time.Since(start).Microseconds()) / 1000})
		fmt.Fprintf(os.Stderr, "%s: %d документов\n", q.ID, len(hits))
	}
	return r, nil
}

// recallAt — доля relevant среди первых k документов выдачи.
func recallAt(relevant, ranked []string, k int) float64 {
	rel := map[string]bool{}
	for _, id := range relevant {
		rel[id] = true
	}
	found := 0
	for _, id := range ranked[:min(k, len(ranked))] {
		if rel[id] {
			found++
		}
	}
	return float64(found) / float64(len(rel))
}

// reciprocalRank — 1/ранг первого relevant в первых depth документах, иначе 0.
func reciprocalRank(relevant, ranked []string, depth int) float64 {
	rel := map[string]bool{}
	for _, id := range relevant {
		rel[id] = true
	}
	for i, id := range ranked[:min(depth, len(ranked))] {
		if rel[id] {
			return 1 / float64(i+1)
		}
	}
	return 0
}

func score(runPath, gradesPath, queriesPath string) (string, error) {
	if runPath == "" {
		return "", fmt.Errorf("укажите -run")
	}
	runData, err := os.ReadFile(runPath)
	if err != nil {
		return "", err
	}
	var r Run
	if err := json.Unmarshal(runData, &r); err != nil {
		return "", fmt.Errorf("%s: %w", runPath, err)
	}
	if r.Mode != "retrieval" && r.Mode != "rag" {
		return "", fmt.Errorf("неизвестный mode прогона %q", r.Mode)
	}
	// Корпус для проверки состава берём из хешей прогона: вопросы перечитываем и сверяем хеш.
	qdata, err := os.ReadFile(queriesPath)
	if err != nil {
		return "", err
	}
	if sum(qdata) != r.QueriesSHA256 {
		return "", fmt.Errorf("файл вопросов изменился после прогона (хеш не совпадает)")
	}
	var qs []Query
	if err := json.Unmarshal(qdata, &qs); err != nil {
		return "", err
	}
	byID := map[string]Result{}
	for _, res := range r.Results {
		if _, dup := byID[res.ID]; dup {
			return "", fmt.Errorf("прогон: дубликат результата %q", res.ID)
		}
		if res.Error != "" {
			if gradesPath != "" || r.Mode == "rag" {
				return "", fmt.Errorf("прогон неполный: вопрос %s завершился ошибкой %q", res.ID, res.Error)
			}
		}
		byID[res.ID] = res
	}
	if len(byID) != len(qs) {
		return "", fmt.Errorf("в прогоне %d результатов, в наборе %d вопросов", len(byID), len(qs))
	}
	for _, q := range qs {
		if _, ok := byID[q.ID]; !ok {
			return "", fmt.Errorf("в прогоне нет вопроса %s", q.ID)
		}
	}

	var grades map[string]Grade
	if gradesPath != "" {
		if r.Mode != "rag" {
			return "", fmt.Errorf("оценки принимаются только для прогона rag")
		}
		if grades, err = loadGrades(gradesPath, r, sum(runData), qs); err != nil {
			return "", err
		}
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "# Результаты benchmark\n\nПрогон `%s` (mode `%s`), старт %s.\n", r.RunID, r.Mode, r.StartedAt)
	model := r.Model
	if model == "" {
		model = "—"
	}
	fmt.Fprintf(&sb, "\nМодель: %s. Параметры: k=%d, k1=%g, b=%g, глубина MRR=%d (полная выдача). Хеш корпуса `%s`.\n", model, r.K, r.K1, r.B, r.MRRDepth, r.CorpusSHA256)

	var recall, mrr float64
	var nNormal, nTrap, correctNormal, rejected int
	var table, errs strings.Builder
	for _, q := range qs {
		ranked := make([]string, len(byID[q.ID].Hits))
		for i, h := range byID[q.ID].Hits {
			ranked[i] = h.Document.ID
		}
		if q.Kind == "trap" {
			nTrap++
			if g, ok := grades[q.ID]; ok && g.Correct {
				rejected++
			} else if ok {
				fmt.Fprintf(&errs, "- %s (trap): нет корректного отказа. %s\n", q.ID, g.Comment)
			}
			continue
		}
		nNormal++
		rc, rr := recallAt(q.Relevant, ranked, r.K), reciprocalRank(q.Relevant, ranked, r.MRRDepth)
		recall += rc
		mrr += rr
		if rc < 1 {
			fmt.Fprintf(&errs, "- %s: Recall@%d = %.2f, топ-%d: %s; relevant: %s.\n", q.ID, r.K, rc, r.K, strings.Join(ranked[:min(r.K, len(ranked))], ", "), strings.Join(q.Relevant, ", "))
		}
		mark := "не оценено"
		if g, ok := grades[q.ID]; ok {
			mark = map[bool]string{true: "да", false: "нет"}[g.Correct]
			if g.Correct {
				correctNormal++
			} else {
				fmt.Fprintf(&errs, "- %s: ответ неполный или неверный. %s\n", q.ID, g.Comment)
			}
		}
		fmt.Fprintf(&table, "| %s | %s | %.2f | %.3f | %s |\n", q.ID, q.Theme, rc, rr, mark)
	}
	fmt.Fprintf(&sb, "\n## Метрики\n\n| Метрика | Значение | Расчёт |\n|---|---|---|\n")
	fmt.Fprintf(&sb, "| Recall@%d | %.3f | %.3f / %d |\n", r.K, recall/float64(nNormal), recall, nNormal)
	fmt.Fprintf(&sb, "| MRR | %.3f | %.3f / %d |\n", mrr/float64(nNormal), mrr, nNormal)
	if grades == nil {
		fmt.Fprintf(&sb, "| Accuracy | не оценено | ?/%d |\n| CRR | не оценено | ?/%d |\n", nNormal, nTrap)
	} else {
		fmt.Fprintf(&sb, "| Accuracy | %.3f | %d / %d |\n| CRR | %.3f | %d / %d |\n", float64(correctNormal)/float64(nNormal), correctNormal, nNormal, float64(rejected)/float64(nTrap), rejected, nTrap)
	}
	fmt.Fprintf(&sb, "\n## Вопросы normal\n\n| ID | Тема | Recall@%d | RR | Ответ полностью верен |\n|---|---|---|---|---|\n%s", r.K, table.String())
	if errs.Len() > 0 {
		fmt.Fprintf(&sb, "\n## Разбор ошибок\n\n%s", errs.String())
	}
	return sb.String(), nil
}

func loadGrades(path string, r Run, runSHA string, qs []Query) (map[string]Grade, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var g Grades
	if err := json.Unmarshal(data, &g); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if g.RunID != r.RunID || g.RunSHA256 != runSHA {
		return nil, fmt.Errorf("оценки относятся к другому прогону (run_id или run_sha256 не совпадает)")
	}
	known := map[string]bool{}
	for _, q := range qs {
		known[q.ID] = true
	}
	res := map[string]Grade{}
	for _, gr := range g.Grades {
		if !known[gr.ID] {
			return nil, fmt.Errorf("оценка для неизвестного вопроса %q", gr.ID)
		}
		if _, dup := res[gr.ID]; dup {
			return nil, fmt.Errorf("дубликат оценки %q", gr.ID)
		}
		res[gr.ID] = gr
	}
	if len(res) != len(qs) {
		return nil, fmt.Errorf("оценок %d, нужно %d (по одной на каждый вопрос)", len(res), len(qs))
	}
	return res, nil
}
