# Интерфейс КТ2: контракт для Николая и Петра

Владелец пакета `internal/ui` — Пётр. Пакет не обращается к БД, поиску или модели и не проверяет права: это работа HTTP-обработчиков Николая. Все типы ниже уже реализованы в этой ветке. Модуль: `github.com/pdd-tech/pdd-assistant`.

## API пакета

```go
renderer, err := ui.New() // один раз при старте; ошибка останавливает запуск
mux.Handle("GET /static/", renderer.StaticHandler())
// после проверки сессии, роли и CSRF в обработчике:
err = renderer.Render(w, http.StatusOK, ui.PageAsk, ui.Data{
    CurrentUser: &ui.CurrentUser{Login: login, Role: "student"},
    CSRFToken: token,
})
```

`Render(w http.ResponseWriter, status int, page Page, data Data) error` буферизует HTML до записи заголовков, возвращает ошибку для неизвестного экрана или недопустимого состояния ответа. При успехе выставляет `Content-Type: text/html; charset=utf-8` и заданный HTTP-статус. Шаблоны загружаются из `embed`, без путей относительно текущего каталога. При ошибке Render обработчик пишет в серверный лог и возвращает HTTP 500 с общим сообщением. Нельзя отправлять клиенту текст внутренней ошибки.

`StaticHandler() http.Handler` обслуживает `/static/style.css` и `/static/forms.js`; в mux монтировать именно с `/static/`, повторно StripPrefix не делать.

## Страницы и данные

| Page | Имя | Данные | HTTP-статус в продукте |
|---|---|---|---|
| `PageLogin` | login | Login, Error, CSRFToken | GET 200, неверный вход 401 |
| `PageAsk` | ask | CurrentUser, Question, Error, CSRFToken | 200 или ошибка ввода 422 |
| `PageAnswer` | answer | CurrentUser, Question, Answer, AnswerState, Sources, CSRFToken | 200; llm_error — 502/504 |
| `PageUsers` | users | CurrentUser, Login, Error/Success, Users, CSRFToken | 200, конфликт логина 409, ввод 422 |
| `PageLog` | log | CurrentUser, Logs, CSRFToken | 200 |
| `PageError` | error | CurrentUser (опционально), Title, Error, CSRFToken | 403/404/500/501 |

`Data` содержит только строки/типизированные структуры, без `template.HTML`. Дополнительно `Preview bool` включает баннер просмотра с GET-ссылками на состояния; в реальном сервере оставлять false.

- `CurrentUser{Login, Role string}`: Role — `student` или `admin`. Шапка строится по роли, но это не замена проверке прав в middleware.
- `Source{ID, Ref, Text string}`: порядок ровно как в выдаче BM25. ID показывается рядом со ссылкой, исходный Text раскрывается в `<details>`. Никаких демонстрационных пунктов пакет не добавляет.
- `User{Login, CreatedAt string}`, `LogEntry{Login, Question, CreatedAt string}`. Время форматирует сервер; для команды удобно `02.01.2006 15:04` в Europe/Moscow. Сортировка по created_at DESC выполняется в store.
- `Question` — вопрос пользователя; на PageAsk сохраняется после ошибки, на PageAnswer отображается отдельно. Поле нового вопроса на PageAnswer пустое.
- `Login` — введённый логин после ошибки. Пароля среди полей Data нет.
- `Error`, `Success` — безопасные сообщения для пользователя, без технических деталей.

### Состояния ответа

| AnswerState | Константа Go | Что передать |
|---|---|---|
| answered | `ui.Answered` | непустой Answer и непустой Sources |
| no_results | `ui.NoResults` | пустой Sources; LLM не вызывается |
| insufficient_context | `ui.InsufficientContext` | отказ модели в Answer (опционально), найденные Sources сохраняются |
| llm_error | `ui.LLMError` | найденные Sources; внутреннюю ошибку не передавать в Answer |

Пустое/неизвестное состояние не трактуется как успех. NoResults с источниками и Answered без ответа/источников возвращают ошибку Render. Недостаточность контекста возможна при непустой выдаче: не выводить тогда ложное «ничего не найдено». Сетевой таймаут не выдавать за корректный отказ.

### Формы

| Действие | method/action | Поля |
|---|---|---|
| Войти | POST /login | login, password, csrf_token |
| Задать вопрос | POST /ask | question, csrf_token |
| Создать ученика | POST /admin/users | login, password, csrf_token |
| Выйти | POST /logout | csrf_token |

Каждый POST содержит hidden `csrf_token` из Data. Токен создаёт/проверяет сервер (включая вход). HTML не создаёт токены. Пароль — input type=password, не сохраняется в Data, не попадает в URL. Поле question required, maxlength=2000. JS отклоняет одни пробелы, блокирует повторную отправку, показывает ожидание и сбрасывает блокировку при возврате через Back. Без JS формы продолжают работать, все проверки повторяет сервер.

Ответы модели выводятся обычным текстом с переносами, HTML/Markdown не исполняются. Если нужен кликабельный источник, пользователь раскрывает документ под ответом. В этой версии нет сторонних скриптов, CDN и шрифтов.

## Самостоятельная проверка

`go run ./cmd/ui-preview` — только просмотр. GET-ссылки в баннере открывают login, login-error, ask, ask-error, answer, no-results, refused, llm-error, users, users-empty, users-error, users-created, log, log-empty, forbidden. Данные явно демонстрационные, не являются корпусом/юридическими эталонами. POST возвращает HTTP 501 с объяснением; регистрации, хранения и генерации нет. Нельзя показывать preview как работающий RAG.

`go test ./...` проверяет рендеринг, экранирование, формы, состояния ответа, embedded-ресурсы и ограничение preview. `go test -race ./...` проверяет совместное использование Renderer. Поиск не менялся, benchmark в этой ветке не существует.

При запуске preview по умолчанию слушает `127.0.0.1:0`: свободный порт выделяет ОС при net.Listen. URL с фактическим портом выводится только после успешного открытия listener. Фиксированный адрес можно передать через -addr; если он занят, процесс завершается с ошибкой и не сообщает об успешном старте. Открывать нужно напечатанный адрес с 127.0.0.1, чтобы избежать путаницы с IPv6 localhost и другими локальными сервисами.
