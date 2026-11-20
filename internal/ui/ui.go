// Package ui отрисовывает интерфейс; права и данные проверяет вызывающий сервер.
package ui

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed templates/*.html static/*
var assets embed.FS

type Page string

const (
	PageLogin  Page = "login"
	PageAsk    Page = "ask"
	PageAnswer Page = "answer"
	PageUsers  Page = "users"
	PageLog    Page = "log"
	PageError  Page = "error"
)

type AnswerState string

const (
	Answered            AnswerState = "answered"
	NoResults           AnswerState = "no_results"
	InsufficientContext AnswerState = "insufficient_context"
	LLMError            AnswerState = "llm_error"
)

type CurrentUser struct{ Login, Role string }
type Source struct{ ID, Ref, Text string }
type User struct{ Login, CreatedAt string }
type LogEntry struct{ Login, Question, CreatedAt string }

type Data struct {
	CurrentUser *CurrentUser
	CSRFToken   string
	Login       string
	Question    string
	Answer      string
	AnswerState AnswerState
	Sources     []Source
	Users       []User
	Logs        []LogEntry
	Error       string
	Success     string
	Title       string
	Preview     bool
}

// Renderer после New неизменяем и может использоваться из нескольких goroutine.
type Renderer struct{ pages map[Page]*template.Template }

func New() (*Renderer, error) {
	r := &Renderer{pages: make(map[Page]*template.Template)}
	for _, p := range []Page{PageLogin, PageAsk, PageAnswer, PageUsers, PageLog, PageError} {
		t, err := template.ParseFS(assets, "templates/base.html", "templates/"+string(p)+".html")
		if err != nil {
			return nil, fmt.Errorf("шаблон %s: %w", p, err)
		}
		r.pages[p] = t
	}
	return r, nil
}

func (r *Renderer) Render(w http.ResponseWriter, status int, page Page, data Data) error {
	t, ok := r.pages[page]
	if !ok {
		return fmt.Errorf("неизвестный экран %q", page)
	}
	if page == PageAnswer {
		switch data.AnswerState {
		case Answered:
			if strings.TrimSpace(data.Answer) == "" || len(data.Sources) == 0 {
				return fmt.Errorf("ответ требует текста и источников")
			}
		case NoResults:
			if len(data.Sources) != 0 {
				return fmt.Errorf("пустая выдача не может содержать источники")
			}
		case InsufficientContext, LLMError:
		default:
			return fmt.Errorf("неизвестное состояние ответа %q", data.AnswerState)
		}
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "base", data); err != nil {
		return fmt.Errorf("рендеринг %s: %w", page, err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, err := w.Write(buf.Bytes())
	return err
}

// StaticHandler монтируется на GET /static/ без дополнительного StripPrefix.
func (r *Renderer) StaticHandler() http.Handler {
	static, err := fs.Sub(assets, "static")
	if err != nil {
		panic(err)
	} // каталог проверяется компилятором через go:embed
	return http.StripPrefix("/static/", http.FileServer(http.FS(static)))
}
