// Package search builds an in-memory BM25 index over the local corpus.
package search

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/kljensen/snowball/russian"
)

type Document struct {
	ID   string `json:"id"`
	Ref  string `json:"ref"`
	Text string `json:"text"`
}

type Hit struct {
	Document Document `json:"document"`
	Score    float64  `json:"score"`
}

type posting struct {
	doc int
	tf  int
}

// Index is immutable after construction and supports concurrent searches.
type Index struct {
	docs     []Document
	postings map[string][]posting
	lengths  []float64
	avgdl    float64
	k1, b    float64
}

func LoadCorpus(dir string) ([]Document, error) {
	var docs []Document
	for _, name := range []string{"koap.json", "pdd.json"} {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var part []Document
		if err := json.Unmarshal(data, &part); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if part == nil {
			return nil, fmt.Errorf("%s: ожидается массив документов", path)
		}
		docs = append(docs, part...)
	}
	if err := validateDocuments(docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func validateDocuments(docs []Document) error {
	if len(docs) == 0 {
		return fmt.Errorf("пустой корпус")
	}
	seen := make(map[string]bool, len(docs))
	for n, doc := range docs {
		if strings.TrimSpace(doc.ID) == "" || strings.TrimSpace(doc.Ref) == "" || strings.TrimSpace(doc.Text) == "" {
			return fmt.Errorf("документ %d: требуются id, ref и text", n+1)
		}
		if seen[doc.ID] {
			return fmt.Errorf("дубликат документа %q", doc.ID)
		}
		seen[doc.ID] = true
	}
	return nil
}

var tokenPattern = regexp.MustCompile(`\p{Nd}+(?:\.\p{Nd}+)+|[\pL\p{Nd}]+`)

func terms(s string) []string {
	tokens := tokenPattern.FindAllString(strings.ReplaceAll(strings.ToLower(s), "ё", "е"), -1)
	result := tokens[:0]
	for _, word := range tokens {
		if !russian.IsStopWord(word) {
			result = append(result, russian.Stem(word, false))
		}
	}
	return result
}

func New(docs []Document, k1, b float64) (*Index, error) {
	if !(k1 > 0 && !math.IsInf(k1, 0) && b >= 0 && b <= 1) {
		return nil, fmt.Errorf("требуются конечные k1 > 0 и b от 0 до 1")
	}
	if err := validateDocuments(docs); err != nil {
		return nil, err
	}
	idx := &Index{docs: append([]Document(nil), docs...), postings: make(map[string][]posting), lengths: make([]float64, len(docs)), k1: k1, b: b}
	total := 0
	for i, doc := range docs {
		tokens := terms(doc.Text)
		idx.lengths[i] = float64(len(tokens))
		total += len(tokens)
		counts := make(map[string]int)
		for _, word := range tokens {
			counts[word]++
		}
		for word, tf := range counts {
			idx.postings[word] = append(idx.postings[word], posting{i, tf})
		}
	}
	idx.avgdl = float64(total) / float64(len(docs))
	return idx, nil
}

func (idx *Index) Search(query string, k int) []Hit {
	hits := []Hit{}
	if k <= 0 || idx.avgdl == 0 {
		return hits
	}
	scores := make(map[int]float64)
	for _, word := range terms(query) {
		postings := idx.postings[word]
		df := float64(len(postings))
		idf := math.Log1p((float64(len(idx.docs)) - df + .5) / (df + .5))
		for _, p := range postings {
			tf := float64(p.tf)
			norm := 1 - idx.b + idx.b*idx.lengths[p.doc]/idx.avgdl
			scores[p.doc] += idf * tf * (idx.k1 + 1) / (tf + idx.k1*norm)
		}
	}
	order := make([]int, 0, len(scores))
	for i := range scores {
		order = append(order, i)
	}
	sort.Slice(order, func(a, b int) bool {
		i, j := order[a], order[b]
		if scores[i] == scores[j] {
			return i < j
		}
		return scores[i] > scores[j]
	})
	for _, i := range order[:min(k, len(order))] {
		hits = append(hits, Hit{idx.docs[i], scores[i]})
	}
	return hits
}
