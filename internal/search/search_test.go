package search

import (
	"math"
	"reflect"
	"testing"
)

func TestSearch(t *testing.T) {
	docs := []Document{{"a", "A", "мост мост"}, {"b", "B", "мост"}, {"c", "C", "мост"}, {"d", "D", "дорога"}}
	index, err := New(docs, 1.2, .75)
	if err != nil {
		t.Fatal(err)
	}
	hits := index.Search("мосту мост", 9)
	if len(hits) != 3 || hits[0].Document.ID != "a" || hits[1].Document.ID != "b" || hits[2].Document.ID != "c" {
		t.Fatalf("неожиданный порядок: %+v", hits)
	}
	want := 2 * math.Log(1+1.5/3.5) * (2 * 2.2) / (2 + 1.2*(.25+.75*2/1.25))
	if math.Abs(hits[0].Score-want) > 1e-12 {
		t.Fatalf("score=%g, ожидалось %g", hits[0].Score, want)
	}
	if !reflect.DeepEqual(hits, index.Search("мосту мост", 9)) {
		t.Fatal("выдача меняется между запросами")
	}
	for _, q := range []string{"", "на и", "космос"} {
		if len(index.Search(q, 5)) != 0 {
			t.Fatalf("ожидалась пустая выдача для %q", q)
		}
	}
	if len(index.Search("мост", 0)) != 0 || len(index.Search("мост", 1)) != 1 {
		t.Fatal("неверное ограничение k")
	}
}

func TestTerms(t *testing.T) {
	want := []string{"перекресток", "12.9", "5.19.1", "пункт", "3"}
	if got := terms("На ПЕРЕКРЁСТОК: 12.9, 5.19.1; пункт-3."); !reflect.DeepEqual(got, want) {
		t.Fatalf("%v, ожидалось %v", got, want)
	}
}

func TestNewRejectsInvalidInput(t *testing.T) {
	doc := Document{"id", "ссылка", "текст"}
	for _, docs := range [][]Document{nil, {doc, doc}, {{"", "ссылка", "текст"}}} {
		if _, err := New(docs, 1.2, .75); err == nil {
			t.Fatalf("принят корпус %v", docs)
		}
	}
	for _, params := range [][2]float64{{0, .75}, {math.Inf(1), .75}, {1.2, math.NaN()}, {1.2, 1.1}} {
		if _, err := New([]Document{doc}, params[0], params[1]); err == nil {
			t.Fatalf("приняты параметры %v", params)
		}
	}
}
