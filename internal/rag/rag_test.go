package rag

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/pdd-tech/pdd-assistant/internal/llm"
	"github.com/pdd-tech/pdd-assistant/internal/search"
)

func TestAskSendsOnlyTopKAndReturnsFullRanking(t *testing.T) {
	model := modelFunc(func(_ context.Context, question string, docs []search.Document) (llm.Response, error) {
		if question != "поворот" || len(docs) != 1 || docs[0].ID != "first" {
			t.Fatalf("unexpected input for model: %q, %+v", question, docs)
		}
		return llm.Response{Answer: "Ответ из первого документа"}, nil
	})
	result, err := testService(t, model).Ask(context.Background(), "поворот")
	if err != nil {
		t.Fatal(err)
	}
	if result.Answer != "Ответ из первого документа" || result.Refused {
		t.Errorf("unexpected answer: %+v", result)
	}
	if got := hitIDs(result.Sources); !slices.Equal(got, []string{"first"}) {
		t.Errorf("sources: %v", got)
	}
	if got := hitIDs(result.Ranked); !slices.Equal(got, []string{"first", "second"}) {
		t.Errorf("full ranking: %v", got)
	}
}

func TestAskSkipsModelForNoMatches(t *testing.T) {
	model := modelFunc(func(context.Context, string, []search.Document) (llm.Response, error) {
		t.Fatal("model was called without sources")
		return llm.Response{}, nil
	})
	result, err := testService(t, model).Ask(context.Background(), "несовпадение")
	if err != nil || !result.Refused || result.Answer == "" || len(result.Sources) != 0 || len(result.Ranked) != 0 {
		t.Fatalf("expected refusal without sources, got %+v, %v", result, err)
	}
}

func TestAskKeepsSourcesWhenModelRefuses(t *testing.T) {
	model := modelFunc(func(context.Context, string, []search.Document) (llm.Response, error) {
		return llm.Response{Answer: "Сведений недостаточно", Refused: true}, nil
	})
	result, err := testService(t, model).Ask(context.Background(), "поворот")
	if err != nil || !result.Refused || result.Answer != "Сведений недостаточно" || len(result.Sources) != 1 {
		t.Fatalf("expected refusal with sources, got %+v, %v", result, err)
	}
}

func TestAskKeepsSourcesOnModelError(t *testing.T) {
	model := modelFunc(func(context.Context, string, []search.Document) (llm.Response, error) {
		return llm.Response{}, context.DeadlineExceeded
	})
	result, err := testService(t, model).Ask(context.Background(), "поворот")
	if !errors.Is(err, context.DeadlineExceeded) || result.Refused || result.Answer != "" {
		t.Errorf("expected model error, got %+v, %v", result, err)
	}
	if len(result.Sources) != 1 || len(result.Ranked) != 2 {
		t.Errorf("result lost sources or full ranking: %+v", result)
	}
}

type modelFunc func(context.Context, string, []search.Document) (llm.Response, error)

func (f modelFunc) Complete(ctx context.Context, question string, docs []search.Document) (llm.Response, error) {
	return f(ctx, question, docs)
}

func testService(t *testing.T, model Model) *Service {
	t.Helper()
	idx, err := search.New([]search.Document{
		{ID: "first", Ref: "ПДД, п. 8.1", Text: "поворот налево"},
		{ID: "second", Ref: "ПДД, п. 8.2", Text: "поворот направо"},
	}, 1.2, 0.75)
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(idx, 1, 2, model)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func hitIDs(hits []search.Hit) []string {
	ids := make([]string, 0, len(hits))
	for _, hit := range hits {
		ids = append(ids, hit.Document.ID)
	}
	return ids
}
