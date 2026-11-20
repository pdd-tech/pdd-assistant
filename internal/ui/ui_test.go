package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func renderer(t *testing.T) *Renderer {
	t.Helper()
	r, err := New()
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRenderingEscapesUntrustedText(t *testing.T) {
	r := renderer(t)
	payload := `<img src=x onerror="alert(1)">`
	d := Data{CurrentUser: &CurrentUser{Login: payload, Role: "student"}, CSRFToken: `token"<&`, Question: payload, Answer: payload, AnswerState: Answered, Sources: []Source{{ID: payload, Ref: payload, Text: payload}}}
	w := httptest.NewRecorder()
	if err := r.Render(w, http.StatusOK, PageAnswer, d); err != nil {
		t.Fatal(err)
	}
	body := w.Body.String()
	if strings.Contains(body, "<img") || !strings.Contains(body, "&lt;img") {
		t.Fatal("небезопасный текст в HTML")
	}
	if !strings.Contains(body, `name="csrf_token" value="token&#34;&lt;&amp;"`) {
		t.Fatal("CSRF не экранирован в атрибуте")
	}
	if got := w.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatal(got)
	}
	for _, page := range []Page{PageLogin, PageAsk, PageUsers, PageLog, PageError} {
		t.Run(string(page), func(t *testing.T) {
			data := Data{Login: payload, Question: payload, Error: payload, Success: payload, Title: payload, Users: []User{{Login: payload, CreatedAt: payload}}, Logs: []LogEntry{{Login: payload, Question: payload, CreatedAt: payload}}}
			out := httptest.NewRecorder()
			if err := r.Render(out, 422, page, data); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out.Body.String(), "<img") {
				t.Fatal("XSS")
			}
			if out.Code != 422 {
				t.Fatal(out.Code)
			}
		})
	}
}

func TestFormsAndServerData(t *testing.T) {
	r := renderer(t)
	for _, tc := range []struct {
		page   Page
		action string
		fields []string
	}{
		{PageLogin, "/login", []string{"login", "password"}},
		{PageAsk, "/ask", []string{"question"}},
		{PageUsers, "/admin/users", []string{"login", "password"}},
	} {
		t.Run(string(tc.page), func(t *testing.T) {
			w := httptest.NewRecorder()
			d := Data{Login: "real-login", CSRFToken: "server-token", Question: "Сохранённый вопрос", CurrentUser: &CurrentUser{Login: "real-user", Role: "admin"}}
			if err := r.Render(w, 200, tc.page, d); err != nil {
				t.Fatal(err)
			}
			body := w.Body.String()
			for _, wanted := range append(tc.fields, "csrf_token") {
				if !strings.Contains(body, `name="`+wanted+`"`) {
					t.Fatalf("поле %s не найдено", wanted)
				}
			}
			for _, wanted := range []string{`method="post"`, `action="` + tc.action + `"`, `value="server-token"`, "real-user", `action="/logout"`} {
				if !strings.Contains(body, wanted) {
					t.Fatal(wanted)
				}
			}
			if strings.Contains(body, "demo-student") || strings.Contains(body, "Просмотр интерфейса КТ2") {
				t.Fatal("демоданные попали в продукт")
			}
			if tc.page == PageAsk && !strings.Contains(body, ">Сохранённый вопрос</textarea>") {
				t.Fatal("вопрос потерян после ошибки")
			}
			if strings.Contains(body, `name="password" value=`) {
				t.Fatal("пароль возвращается в HTML")
			}
		})
	}
}

func TestAnswerStatesAndSources(t *testing.T) {
	r := renderer(t)
	for _, tc := range []struct {
		state   AnswerState
		message string
		sources bool
	}{
		{Answered, "Реальный ответ", true},
		{NoResults, "ничего не нашлось", false},
		{InsufficientContext, "В документах нет этой информации", true},
		{LLMError, "Не удалось получить ответ", true},
	} {
		t.Run(string(tc.state), func(t *testing.T) {
			d := Data{AnswerState: tc.state, Question: "Вопрос", Answer: "Реальный ответ"}
			if tc.sources {
				d.Sources = []Source{{ID: "first", Ref: "Первый", Text: "Полный текст первого"}, {ID: "second", Ref: "Второй", Text: "Полный текст второго"}}
			}
			w := httptest.NewRecorder()
			if err := r.Render(w, 200, PageAnswer, d); err != nil {
				t.Fatal(err)
			}
			body := w.Body.String()
			if !strings.Contains(body, tc.message) {
				t.Fatal(body)
			}
			if tc.sources {
				if !strings.Contains(body, "Полный текст первого") || strings.Index(body, "Первый") >= strings.Index(body, "Второй") {
					t.Fatal("источники потеряны или переставлены")
				}
			} else if strings.Contains(body, "Найденные пункты") {
				t.Fatal("источники придуманы")
			}
			if tc.state != Answered && tc.state != InsufficientContext && strings.Contains(body, "Реальный ответ") {
				t.Fatal("ответ показан в ошибочном состоянии")
			}
		})
	}
}

func TestInvalidRenderWritesNothing(t *testing.T) {
	r := renderer(t)
	for _, tc := range []struct {
		page Page
		data Data
	}{
		{Page("unknown"), Data{}},
		{PageAnswer, Data{}},
		{PageAnswer, Data{AnswerState: "unknown"}},
		{PageAnswer, Data{AnswerState: Answered}},
		{PageAnswer, Data{AnswerState: NoResults, Sources: []Source{{ID: "bad"}}}},
	} {
		w := httptest.NewRecorder()
		if r.Render(w, 200, tc.page, tc.data) == nil {
			t.Fatal("ожидалась ошибка")
		}
		if w.Body.Len() != 0 || w.Header().Get("Content-Type") != "" {
			t.Fatal("ошибка после отправки HTML")
		}
	}
}

func TestEmptyListsAndNavigation(t *testing.T) {
	r := renderer(t)
	for _, tc := range []struct {
		page    Page
		message string
	}{
		{PageUsers, "Учеников пока нет"}, {PageLog, "Ученики пока не задавали вопросов"},
	} {
		w := httptest.NewRecorder()
		if err := r.Render(w, 200, tc.page, Data{}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(w.Body.String(), tc.message) {
			t.Fatal("нет пустого состояния")
		}
	}
	w := httptest.NewRecorder()
	if err := r.Render(w, 200, PageAsk, Data{CurrentUser: &CurrentUser{Login: "student", Role: "student"}}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(w.Body.String(), "/admin/users") {
		t.Fatal("админская навигация у ученика")
	}
}

func TestEmbeddedResources(t *testing.T) {
	r := renderer(t)
	for _, path := range []string{"/static/style.css", "/static/forms.js"} {
		w := httptest.NewRecorder()
		r.StaticHandler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || w.Body.Len() == 0 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	r.StaticHandler().ServeHTTP(w, httptest.NewRequest("GET", "/static/missing.css", nil))
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
}

func TestRendererConcurrentUse(t *testing.T) {
	r := renderer(t)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := r.Render(httptest.NewRecorder(), 200, PageAsk, Data{}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}
