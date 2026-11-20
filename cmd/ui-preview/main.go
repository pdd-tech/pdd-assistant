// ui-preview показывает шаблоны без авторизации, хранилища, поиска и LLM.
package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/pdd-tech/pdd-assistant/internal/ui"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "адрес локального просмотра")
	flag.Parse()
	r, err := ui.New()
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Просмотр UI: http://%s — демонстрационные данные, без RAG и сохранения", *addr)
	s := &http.Server{Addr: *addr, Handler: previewHandler(r), ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(s.ListenAndServe())
}

func previewHandler(r *ui.Renderer) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", r.StaticHandler())
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		d := ui.Data{Preview: true}
		page, status := ui.PageError, http.StatusOK
		if req.Method != http.MethodGet {
			status, d.Title = http.StatusNotImplemented, "Это просмотр интерфейса"
			d.Error = "Обработчики форм ещё не подключены. Для входа, сохранения и RAG нужен сервер интеграции. Выберите экран по ссылкам в баннере."
		} else {
			page, status, d = fixture(req.URL.Path, req.URL.Query().Get("state"))
		}
		if err := r.Render(w, status, page, d); err != nil {
			log.Print(err)
			http.Error(w, "Не удалось показать страницу", http.StatusInternalServerError)
		}
	})
	return mux
}

func fixture(path, state string) (ui.Page, int, ui.Data) {
	d := ui.Data{Preview: true}
	student := &ui.CurrentUser{Login: "demo-student", Role: "student"}
	admin := &ui.CurrentUser{Login: "demo-admin", Role: "admin"}
	page, status := ui.PageError, http.StatusOK
	switch path {
	case "/login":
		page = ui.PageLogin
		if state == "error" {
			d.Login, d.Error = "demo-student", "Неверный логин или пароль"
			status = http.StatusUnauthorized
		}
	case "/":
		page, d.CurrentUser = ui.PageAsk, student
		if state == "error" {
			d.Question, d.Error = "Сохранённый вопрос после ошибки ввода", "Вопрос должен содержать от 1 до 2000 символов"
			status = http.StatusUnprocessableEntity
		}
	case "/ask":
		page, d.CurrentUser = ui.PageAnswer, student
		d.Question = "Как выглядит ответ с найденными документами?"
		d.AnswerState, d.Answer = ui.Answered, "Это демонстрация оформления ответа. В рабочем приложении здесь будет текст языковой модели со ссылками на переданные пункты.\nНапример: (ПДД, п. 11.4)."
		d.Sources = []ui.Source{
			{ID: "demo-pdd-11.4", Ref: "ПДД, п. 11.4 · пример", Text: "Демонстрационный фрагмент для проверки раскрытия документа.\nПолный проверенный текст загрузит модуль поиска из корпуса."},
			{ID: "demo-pdd-term", Ref: "ПДД, термин · пример", Text: "Второй демонстрационный источник. Порядок документов задаёт поисковый модуль."},
		}
		switch state {
		case "empty":
			d.AnswerState, d.Answer, d.Sources = ui.NoResults, "", nil
		case "refused":
			d.AnswerState, d.Answer = ui.InsufficientContext, "В найденных фрагментах нет сведений, достаточных для ответа на этот вопрос."
		case "error":
			d.AnswerState, d.Answer, status = ui.LLMError, "", http.StatusBadGateway
		}
	case "/admin/users":
		page, d.CurrentUser = ui.PageUsers, admin
		d.Users = []ui.User{{Login: "demo-student", CreatedAt: "20.11.2026 12:00"}}
		switch state {
		case "empty":
			d.Users = nil
		case "error":
			d.Login, d.Error, status = "demo-student", "Логин demo-student уже занят", http.StatusConflict
		case "created":
			d.Success = "Ученик demo-new создан"
			d.Users = append([]ui.User{{Login: "demo-new", CreatedAt: "20.11.2026 12:10"}}, d.Users...)
		}
	case "/admin/log":
		page, d.CurrentUser = ui.PageLog, admin
		d.Logs = []ui.LogEntry{{Login: "demo-student", Question: "Пример вопроса в журнале запросов", CreatedAt: "20.11.2026 12:01"}}
		if state == "empty" {
			d.Logs = nil
		}
	case "/forbidden":
		d.CurrentUser, d.Title, d.Error, status = student, "Нет доступа", "Эта страница доступна только администратору.", http.StatusForbidden
	default:
		d.Title, d.Error, status = "Страница не найдена", "Выберите экран по ссылкам в баннере просмотра.", http.StatusNotFound
	}
	return page, status, d
}
