// Package web connects the browser interface to sessions, storage, and RAG.
package web

import (
	"context"
	"errors"
	"log"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pdd-tech/pdd-assistant/internal/auth"
	"github.com/pdd-tech/pdd-assistant/internal/rag"
	"github.com/pdd-tech/pdd-assistant/internal/store"
	"github.com/pdd-tech/pdd-assistant/internal/ui"
)

const (
	maxFormBytes     = 32 << 10
	maxQuestionRunes = 2000
	adminUsers       = "/admin/users"
	loginPath        = "/login"
	timeLayout       = "02.01.2006 15:04"
)

var moscow = time.FixedZone("MSK", 3*60*60)

type Store interface {
	Authenticate(ctx context.Context, login, password string) (store.User, error)
	CreateStudent(ctx context.Context, login, password string) error
	ListStudents(ctx context.Context) ([]store.User, error)
	ListQueries(ctx context.Context) ([]store.Query, error)
	LogQuestion(ctx context.Context, userID int64, question string) error
}

type Asker interface {
	Ask(ctx context.Context, question string) (rag.Result, error)
}

type app struct {
	renderer *ui.Renderer
	db       Store
	sessions *auth.Sessions
	qa       Asker
	logger   *log.Logger
}

type handler func(http.ResponseWriter, *http.Request, auth.Session)

func New(renderer *ui.Renderer, db Store, sessions *auth.Sessions, qa Asker, logger *log.Logger) http.Handler {
	a := &app{renderer: renderer, db: db, sessions: sessions, qa: qa, logger: logger}
	mux := http.NewServeMux()
	mux.Handle("GET /static/", renderer.StaticHandler())
	mux.HandleFunc("GET /login", a.route(false, "", a.loginPage))
	mux.HandleFunc("POST /login", a.route(false, "", a.login))
	mux.HandleFunc("POST /logout", a.route(true, "", a.logout))
	mux.HandleFunc("GET /{$}", a.route(true, store.RoleStudent, a.askPage))
	mux.HandleFunc("POST /ask", a.route(true, store.RoleStudent, a.ask))
	mux.HandleFunc("GET /admin/users", a.route(true, store.RoleAdmin, a.usersPage))
	mux.HandleFunc("POST /admin/users", a.route(true, store.RoleAdmin, a.createStudent))
	mux.HandleFunc("GET /admin/log", a.route(true, store.RoleAdmin, a.logPage))
	mux.HandleFunc("/", a.route(true, "", a.notFound))
	return mux
}

func (a *app) route(requireLogin bool, role string, next handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		session, _ := a.sessions.Get(r)
		if requireLogin && session.User.ID == 0 {
			http.Redirect(w, r, loginPath, http.StatusSeeOther)
			return
		}
		if role != "" && session.User.Role != role {
			a.errorPage(w, session, http.StatusForbidden, "Нет доступа", "")
			return
		}
		if r.Method == http.MethodPost && !a.readForm(w, r, session) {
			return
		}
		next(w, r, session)
	}
}

func (a *app) readForm(w http.ResponseWriter, r *http.Request, session auth.Session) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" {
		a.errorPage(w, session, http.StatusUnsupportedMediaType, "Не удалось прочитать форму", "")
		return false
	}
	if err := r.ParseForm(); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			a.errorPage(w, session, http.StatusRequestEntityTooLarge, "Форма слишком большая", "")
		} else {
			a.errorPage(w, session, http.StatusBadRequest, "Не удалось прочитать форму", "")
		}
		return false
	}
	tokens := r.PostForm["csrf_token"]
	if len(tokens) != 1 || !auth.ValidCSRF(session, tokens[0]) {
		a.errorPage(w, session, http.StatusForbidden, "Нет доступа", "Обновите страницу и повторите действие.")
		return false
	}
	return true
}

func (a *app) loginPage(w http.ResponseWriter, r *http.Request, session auth.Session) {
	if session.User.ID != 0 {
		redirectHome(w, r, session.User)
		return
	}
	if session.CSRFToken == "" {
		session = a.sessions.Start(w, r, store.User{})
	}
	a.render(w, http.StatusOK, ui.PageLogin, pageData(session))
}

func (a *app) login(w http.ResponseWriter, r *http.Request, session auth.Session) {
	if session.User.ID != 0 {
		redirectHome(w, r, session.User)
		return
	}
	login := strings.TrimSpace(r.PostForm.Get("login"))
	user, err := a.db.Authenticate(r.Context(), login, r.PostForm.Get("password"))
	if errors.Is(err, store.ErrInvalidCredentials) {
		data := pageData(session)
		data.Login = login
		data.Error = "Неверный логин или пароль"
		a.render(w, http.StatusUnauthorized, ui.PageLogin, data)
		return
	}
	if err != nil {
		a.internalError(w, session, "вход", err)
		return
	}
	a.sessions.Start(w, r, user)
	redirectHome(w, r, user)
}

func redirectHome(w http.ResponseWriter, r *http.Request, user store.User) {
	path := "/"
	if user.Role == store.RoleAdmin {
		path = adminUsers
	}
	http.Redirect(w, r, path, http.StatusSeeOther)
}

func (a *app) logout(w http.ResponseWriter, r *http.Request, _ auth.Session) {
	a.sessions.End(w, r)
	http.Redirect(w, r, loginPath, http.StatusSeeOther)
}

func (a *app) askPage(w http.ResponseWriter, _ *http.Request, session auth.Session) {
	a.render(w, http.StatusOK, ui.PageAsk, pageData(session))
}

func (a *app) ask(w http.ResponseWriter, r *http.Request, session auth.Session) {
	data := pageData(session)
	data.Question = strings.TrimSpace(r.PostForm.Get("question"))
	if data.Question == "" || !utf8.ValidString(data.Question) || utf8.RuneCountInString(data.Question) > maxQuestionRunes {
		data.Error = "Введите вопрос длиной от 1 до 2000 символов."
		a.render(w, http.StatusUnprocessableEntity, ui.PageAsk, data)
		return
	}
	// Persist accepted questions before calling the model so a generation failure
	// or cancellation cannot lose the entry.
	if err := a.db.LogQuestion(r.Context(), session.User.ID, data.Question); err != nil {
		a.internalError(w, session, "запись вопроса", err)
		return
	}
	result, err := a.qa.Ask(r.Context(), data.Question)
	for _, hit := range result.Sources {
		doc := hit.Document
		data.Sources = append(data.Sources, ui.Source{ID: doc.ID, Ref: doc.Ref, Text: doc.Text})
	}
	status := http.StatusOK
	switch {
	case err != nil:
		a.logger.Printf("ответ RAG: %v", err)
		status = http.StatusBadGateway
		if errors.Is(err, context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
		}
		data.AnswerState = ui.LLMError
	case len(data.Sources) == 0:
		data.AnswerState = ui.NoResults
	case result.Refused:
		data.AnswerState = ui.InsufficientContext
		data.Answer = result.Answer
	default:
		data.AnswerState = ui.Answered
		data.Answer = result.Answer
	}
	a.render(w, status, ui.PageAnswer, data)
}

func (a *app) usersPage(w http.ResponseWriter, r *http.Request, session auth.Session) {
	a.renderUsers(w, r, session, http.StatusOK, pageData(session))
}

func (a *app) createStudent(w http.ResponseWriter, r *http.Request, session auth.Session) {
	login := strings.TrimSpace(r.PostForm.Get("login"))
	err := a.db.CreateStudent(r.Context(), login, r.PostForm.Get("password"))
	if err == nil {
		query := url.Values{"created": {login}}
		http.Redirect(w, r, adminUsers+"?"+query.Encode(), http.StatusSeeOther)
		return
	}

	data := pageData(session)
	data.Login = login
	switch {
	case errors.Is(err, store.ErrDuplicateLogin):
		data.Error = "Логин " + login + " уже занят"
		a.renderUsers(w, r, session, http.StatusConflict, data)
	case errors.Is(err, store.ErrInvalidInput):
		data.Error = "Проверьте логин и пароль. Логин должен содержать от 1 до 64 символов без пробелов, пароль — от 1 до 72 байт и хотя бы один непробельный символ."
		a.renderUsers(w, r, session, http.StatusUnprocessableEntity, data)
	default:
		a.internalError(w, session, "создание ученика", err)
	}
}

func (a *app) renderUsers(w http.ResponseWriter, r *http.Request, session auth.Session, status int, data ui.Data) {
	students, err := a.db.ListStudents(r.Context())
	if err != nil {
		a.internalError(w, session, "чтение учеников", err)
		return
	}
	created := r.URL.Query().Get("created")
	for _, student := range students {
		data.Users = append(data.Users, ui.User{
			Login: student.Login, CreatedAt: student.CreatedAt.In(moscow).Format(timeLayout),
		})
		if status == http.StatusOK && student.Login == created {
			data.Success = "Ученик " + student.Login + " создан"
		}
	}
	a.render(w, status, ui.PageUsers, data)
}

func (a *app) logPage(w http.ResponseWriter, r *http.Request, session auth.Session) {
	entries, err := a.db.ListQueries(r.Context())
	if err != nil {
		a.internalError(w, session, "чтение журнала", err)
		return
	}
	data := pageData(session)
	for _, entry := range entries {
		data.Logs = append(data.Logs, ui.LogEntry{
			Login: entry.Login, Question: entry.Question, CreatedAt: entry.CreatedAt.In(moscow).Format(timeLayout),
		})
	}
	a.render(w, http.StatusOK, ui.PageLog, data)
}

func pageData(session auth.Session) ui.Data {
	data := ui.Data{CSRFToken: session.CSRFToken}
	if session.User.ID != 0 {
		data.CurrentUser = &ui.CurrentUser{Login: session.User.Login, Role: session.User.Role}
	}
	return data
}

func (a *app) notFound(w http.ResponseWriter, _ *http.Request, session auth.Session) {
	a.errorPage(w, session, http.StatusNotFound, "Страница не найдена", "")
}

func (a *app) internalError(w http.ResponseWriter, session auth.Session, step string, err error) {
	a.logger.Printf("%s: %v", step, err)
	a.errorPage(w, session, http.StatusInternalServerError, "Ошибка сервера", "Попробуйте позже.")
}

func (a *app) errorPage(w http.ResponseWriter, session auth.Session, status int, title, message string) {
	data := pageData(session)
	data.Title = title
	data.Error = message
	a.render(w, status, ui.PageError, data)
}

func (a *app) render(w http.ResponseWriter, status int, page ui.Page, data ui.Data) {
	if err := a.renderer.Render(w, status, page, data); err != nil {
		a.logger.Printf("страница %s: %v", page, err)
		http.Error(w, "Не удалось открыть страницу", http.StatusInternalServerError)
	}
}
