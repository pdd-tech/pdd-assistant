// Command exchange runs our BM25 and RAG on another team's JSONL package.
package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/pdd-tech/pdd-assistant/internal/config"
	"github.com/pdd-tech/pdd-assistant/internal/llm"
	"github.com/pdd-tech/pdd-assistant/internal/rag"
	"github.com/pdd-tech/pdd-assistant/internal/search"
)

type patent struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Text  string `json:"text"`
}
type question struct {
	ID       string `json:"id"`
	Question string `json:"question"`
}
type result struct {
	ID        string       `json:"id"`
	Question  string       `json:"question"`
	Answer    string       `json:"answer"`
	Refused   bool         `json:"refused"`
	Error     string       `json:"error,omitempty"`
	ElapsedMS float64      `json:"elapsed_ms"`
	Sources   []search.Hit `json:"sources"`
	Hits      []search.Hit `json:"hits"`
}
type run struct {
	RunID               string       `json:"run_id"`
	StartedAt           string       `json:"started_at"`
	Status              string       `json:"status"`
	CorpusSHA           string       `json:"input_corpus_sha256"`
	QuestionsSHA        string       `json:"input_questions_sha256"`
	IndexedSHA          string       `json:"indexed_corpus_sha256"`
	ConfigSHA           string       `json:"config_sha256"`
	Documents           int          `json:"documents"`
	Questions           int          `json:"questions"`
	K                   int          `json:"k"`
	K1                  float64      `json:"k1"`
	B                   float64      `json:"b"`
	Depth               int          `json:"ranking_depth"`
	LLM                 llm.Settings `json:"llm"`
	GroundTruthProvided bool         `json:"ground_truth_provided"`
	Results             []result     `json:"results"`
	ElapsedMS           float64      `json:"elapsed_ms"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := execute(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "Ошибка обменного прогона:", err)
		os.Exit(1)
	}
}

func readJSONL[T any](path string) ([]T, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var rows []T
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 4096), 2<<20)
	line := 0
	for scanner.Scan() {
		line++
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var row T
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			return nil, nil, fmt.Errorf("%s строка %d: %w", path, line, err)
		}
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}
	if len(rows) == 0 {
		return nil, nil, fmt.Errorf("пустой JSONL: %s", path)
	}
	return rows, data, nil
}

func load(corpusPath, questionsPath string) ([]search.Document, []question, string, string, error) {
	patents, corpus, err := readJSONL[patent](corpusPath)
	if err != nil {
		return nil, nil, "", "", err
	}
	qs, qdata, err := readJSONL[question](questionsPath)
	if err != nil {
		return nil, nil, "", "", err
	}
	docs := make([]search.Document, 0, len(patents))
	seen := map[string]bool{}
	for _, p := range patents {
		if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Title) == "" || strings.TrimSpace(p.Text) == "" || seen[p.ID] {
			return nil, nil, "", "", fmt.Errorf("пустые поля или дубликат патента %q", p.ID)
		}
		seen[p.ID] = true
		docs = append(docs, search.Document{ID: p.ID, Ref: "Патент " + p.ID + " — " + p.Title, Text: p.Title + "\n" + p.Text})
	}
	seen = map[string]bool{}
	for _, q := range qs {
		if strings.TrimSpace(q.ID) == "" || strings.TrimSpace(q.Question) == "" || seen[q.ID] {
			return nil, nil, "", "", fmt.Errorf("пустые поля или дубликат вопроса %q", q.ID)
		}
		seen[q.ID] = true
	}
	return docs, qs, hash(corpus), hash(qdata), nil
}

func execute(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("exchange", flag.ContinueOnError)
	corpusPath := flags.String("corpus", "", "corpus.jsonl другой команды")
	questionsPath := flags.String("questions", "", "questions.jsonl другой команды")
	out := flags.String("out", "", "новый JSON результатов")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *corpusPath == "" || *questionsPath == "" || *out == "" {
		return fmt.Errorf("нужны -corpus, -questions и -out")
	}
	if _, err := os.Stat(*out); err == nil {
		return fmt.Errorf("файл %s уже существует; выберите новое имя", *out)
	} else if !os.IsNotExist(err) {
		return err
	}
	docs, qs, corpusSHA, questionsSHA, err := load(*corpusPath, *questionsPath)
	if err != nil {
		return err
	}
	index, err := search.New(docs, config.SearchK1, config.SearchB)
	if err != nil {
		return err
	}
	cfg, err := config.LoadLLM()
	if err != nil {
		return err
	}
	cfg.Profile = "patents"
	client, err := llm.New(cfg)
	if err != nil {
		return err
	}
	service, err := rag.New(index, config.SearchK, len(docs), client)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	begin := time.Now()
	r := run{RunID: now.Format("20060102T150405.000000000Z") + "-patent-exchange", StartedAt: now.Format(time.RFC3339Nano), Status: "running", CorpusSHA: corpusSHA, QuestionsSHA: questionsSHA, Documents: len(docs), Questions: len(qs), K: config.SearchK, K1: config.SearchK1, B: config.SearchB, Depth: len(docs), LLM: client.Settings(), Results: make([]result, 0, len(qs))}
	indexed, _ := json.Marshal(docs)
	r.IndexedSHA = hash(indexed)
	configuration, _ := json.Marshal(struct {
		K     int
		K1, B float64
		Depth int
		LLM   llm.Settings
	}{r.K, r.K1, r.B, r.Depth, r.LLM})
	r.ConfigSHA = hash(configuration)
	if err := save(*out, r); err != nil {
		return err
	}
	failures := 0
	for i, q := range qs {
		if ctx.Err() != nil {
			r.Status = "failed"
			r.ElapsedMS = float64(time.Since(begin).Microseconds()) / 1000
			if err := save(*out, r); err != nil {
				return err
			}
			return ctx.Err()
		}
		fmt.Fprintf(os.Stderr, "[%d/%d] %s: запрос\n", i+1, len(qs), q.ID)
		start := time.Now()
		answer, err := service.Ask(ctx, q.Question)
		item := result{ID: q.ID, Question: q.Question, Answer: answer.Answer, Refused: answer.Refused, ElapsedMS: float64(time.Since(start).Microseconds()) / 1000, Sources: answer.Sources, Hits: answer.Ranked}
		state := "ответ"
		if err != nil {
			failures++
			item.Error = err.Error()
			state = "ошибка: " + item.Error
		} else if item.Refused {
			state = "отказ"
			if len(item.Hits) == 0 {
				item.Answer = "В переданном корпусе патентов подходящих рефератов не найдено."
			}
		}
		r.Results = append(r.Results, item)
		r.ElapsedMS = float64(time.Since(begin).Microseconds()) / 1000
		if err := save(*out, r); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "[%d/%d] %s: %s; источников %d; %.1f с\n", i+1, len(qs), q.ID, state, len(item.Sources), item.ElapsedMS/1000)
	}
	r.Status = "completed"
	if failures > 0 {
		r.Status = "failed"
	}
	if err := save(*out, r); err != nil {
		return err
	}
	if failures > 0 {
		return fmt.Errorf("ошибки в %d из %d вопросов; фактические результаты сохранены", failures, len(qs))
	}
	fmt.Fprintf(os.Stderr, "Сохранены все %d ответов: %s\n", len(qs), *out)
	return nil
}

func hash(data []byte) string { s := sha256.Sum256(data); return hex.EncodeToString(s[:]) }
func save(path string, r run) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".exchange-*.json")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if _, err = temp.Write(append(data, '\n')); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
