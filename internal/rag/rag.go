// Package rag runs the same retrieval and generation pipeline for web and benchmark.
package rag

import (
	"context"
	"errors"

	"github.com/pdd-tech/pdd-assistant/internal/llm"
	"github.com/pdd-tech/pdd-assistant/internal/search"
)

type Model interface {
	Complete(ctx context.Context, question string, docs []search.Document) (llm.Response, error)
}

type Service struct {
	index *search.Index
	model Model
	k     int
	depth int
}

type Result struct {
	Answer  string
	Sources []search.Hit // Actual context passed to the model.
	Ranked  []search.Hit // Full ranking for retrieval metrics.
	Refused bool
}

func New(index *search.Index, k, depth int, model Model) (*Service, error) {
	if index == nil || model == nil {
		return nil, errors.New("RAG: нужны индекс и клиент модели")
	}
	if k <= 0 || depth <= 0 {
		return nil, errors.New("RAG: k и глубина поиска должны быть положительными")
	}
	return &Service{index: index, model: model, k: k, depth: max(k, depth)}, nil
}

func (s *Service) Ask(ctx context.Context, question string) (Result, error) {
	ranked := s.index.Search(question, s.depth)
	if len(ranked) == 0 {
		return Result{
			Answer:  "По вашему вопросу ничего не нашлось в ПДД и главе 12 КоАП. Попробуйте сформулировать иначе.",
			Refused: true,
		}, nil
	}

	result := Result{Ranked: ranked, Sources: ranked[:min(s.k, len(ranked))]}
	docs := make([]search.Document, len(result.Sources))
	for i, hit := range result.Sources {
		docs[i] = hit.Document
	}
	answer, err := s.model.Complete(ctx, question, docs)
	if err != nil {
		return result, err
	}
	result.Answer, result.Refused = answer.Answer, answer.Refused
	return result, nil
}
