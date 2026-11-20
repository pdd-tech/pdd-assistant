package main

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pdd-tech/pdd-assistant/internal/ui"
)

func TestPreviewScreensAndMethods(t *testing.T) {
	r, err := ui.New()
	if err != nil {
		t.Fatal(err)
	}
	h := previewHandler(r)
	for _, tc := range []struct {
		method, path string
		status       int
		text         string
	}{
		{"GET", "/login", 200, "Вход в помощник"},
		{"GET", "/login?state=error", 401, "Неверный логин"},
		{"GET", "/", 200, "Задайте вопрос"},
		{"GET", "/?state=error", 422, "Сохранённый вопрос"},
		{"GET", "/ask", 200, "демонстрация оформления ответа"},
		{"GET", "/ask?state=empty", 200, "ничего не нашлось"},
		{"GET", "/ask?state=refused", 200, "В документах нет этой информации"},
		{"GET", "/ask?state=error", 502, "Не удалось получить ответ"},
		{"GET", "/admin/users", 200, "demo-student"},
		{"GET", "/admin/users?state=empty", 200, "Учеников пока нет"},
		{"GET", "/admin/users?state=error", 409, "уже занят"},
		{"GET", "/admin/users?state=created", 200, "demo-new создан"},
		{"GET", "/admin/log", 200, "Пример вопроса в журнале"},
		{"GET", "/admin/log?state=empty", 200, "пока не задавали"},
		{"GET", "/forbidden", 403, "Нет доступа"},
		{"GET", "/missing", 404, "Страница не найдена"},
		{"POST", "/login", 501, "Обработчики форм ещё не подключены"},
		{"POST", "/ask", 501, "Обработчики форм ещё не подключены"},
		{"POST", "/admin/users", 501, "Обработчики форм ещё не подключены"},
		{"POST", "/logout", 501, "Обработчики форм ещё не подключены"},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != tc.status {
				t.Fatalf("статус %d, нужен %d", w.Code, tc.status)
			}
			if !strings.Contains(w.Body.String(), tc.text) || !strings.Contains(w.Body.String(), "Демонстрационные данные") {
				t.Fatal(w.Body.String())
			}
		})
	}
}
