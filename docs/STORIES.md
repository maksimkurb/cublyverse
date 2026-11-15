# Cublyverse — стори

Формат: `ID — название`, зачем, что сделать, критерии приёмки (КП).
Общие критерии для каждой стори: `make check` зелёный (gen без диффа, vet,
тесты), новая логика покрыта тестами, секреты не попадают в репозиторий.

Статусы: ⬜ не начато · 🟡 в работе · ✅ готово. Обновляются в этом файле.

---

## M0 — Каркас

### ✅ M0-1 — Каркас репозитория и конфигурация
Что сделать:
- `cmd/cublyverse/main.go` с подкомандами `serve` и `migrate` (stdlib `flag`,
  без cobra).
- `internal/config`: загрузка из env в структуру с валидацией. Переменные:
  `HTTP_ADDR` (по умолчанию `:8080`), `PUBLIC_BASE_URL`, `DATABASE_URL`,
  `LOG_FORMAT` (`text`|`json`), `OIDC_ISSUER`, `OIDC_CLIENT_ID`,
  `OIDC_CLIENT_SECRET`, `TELEGRAM_BOT_TOKEN`, `MATRIX_*` (как в
  `unibot/cmd/demo`). На этом этапе обязательны только `DATABASE_URL`.
- `log/slog` как единственный логгер.
- `.env.example`, `.gitignore` (уже есть — дополнить при необходимости),
  `Makefile` с целями `gen`, `build`, `test`, `lint`, `check`, `dev`.

КП:
- `go run ./cmd/cublyverse serve` без `DATABASE_URL` завершается с понятной
  ошибкой, перечисляющей недостающие переменные.
- Тест на загрузку конфигурации (значения по умолчанию, ошибка валидации).

### ✅ M0-2 — PostgreSQL: пул, миграции, sqlc
Что сделать:
- `internal/store`: `pgxpool`, функция `Open(ctx, url)`, проверка соединения.
- Миграции `goose` в `internal/store/migrations/*.sql`, встроены через
  `embed`; `cublyverse migrate up|status`; `serve` применяет миграции при
  старте.
- `sqlc.yaml` (engine postgresql, пакет `pgx/v5`), запросы в
  `internal/store/queries/*.sql`, вывод в `internal/store/db`.
- Первая миграция: расширения не нужны; создать таблицу `users`
  (`id uuid pk`, `oidc_issuer text`, `oidc_subject text`, `email text`,
  `display_name text`, `timezone text not null default 'UTC'`,
  `created_at`, `updated_at`, unique `(oidc_issuer, oidc_subject)`).
- Хелпер для интеграционных тестов на `testcontainers-go` (postgres:17);
  тесты пропускаются с `t.Skip`, если Docker недоступен.

КП:
- Интеграционный тест: поднять PG, применить миграции, вставить и прочитать
  пользователя через sqlc.
- `make gen` идемпотентен (повторный запуск не даёт диффа).

### ✅ M0-3 — Конвейер OpenAPI
Что сделать:
- `api/openapi.yaml` (OpenAPI 3.0.3): `info`, сервер `/api/v1`, общая схема
  `Error {code, message}`, операция `GET /health` (`operationId: getHealth`,
  ответ `{status, version}`).
- `oapi-codegen` (через `go tool` / `tools.go`, версия закреплена в `go.mod`):
  конфиг `api/oapi-codegen.yaml`, генерация strict-сервера для `net/http` и
  типов в `internal/httpapi/gen`.
- `internal/httpapi`: реализация strict-интерфейса, единый формат ошибок,
  middleware: request id, логирование запроса, recover.
- Отдача спецификации: `GET /api/openapi.yaml` и страница документации
  `GET /api/docs` (Scalar или Swagger UI с CDN).

КП:
- `GET /api/v1/health` возвращает 200 и JSON по схеме (тест через
  `httptest`).
- Ответ на неизвестный путь и паника в обработчике возвращают `Error` в JSON.
- Правка `openapi.yaml` без `make gen` ловится в `make check`.

### ✅ M0-4 — HTTP-сервер и сборка приложения
Что сделать:
- `internal/app`: сборка зависимостей (config → store → httpapi), запуск и
  корректная остановка по SIGINT/SIGTERM (таймаут 10 с).
- `/healthz` (процесс жив) и `/readyz` (БД отвечает).
- Интерфейсы `Feature` и `Job` из PLAN.md §2 и пустой реестр фич; раннер
  фоновых задач (горутина на задачу, остановка по контексту).

КП:
- `serve` стартует с одной лишь `DATABASE_URL`; бот-транспорты не запускаются,
  если их переменные не заданы (в лог — предупреждение).
- `/readyz` возвращает 503 при недоступной БД (тест).

### ✅ M0-5 — Каркас WebApp и кодген клиента
Что сделать:
- `web/`: Vite + React 18 + TypeScript (strict), pnpm, ESLint, Vitest.
- `pnpm gen`: `openapi-typescript` из `../api/openapi.yaml` в
  `web/src/api/schema.d.ts`; клиент на `openapi-fetch` в `web/src/api/client.ts`.
- Страница-заглушка, которая вызывает `GET /api/v1/health` и показывает версию.
- Go отдаёт `web/dist` по `/app/` из `embed` с SPA-fallback на `index.html`;
  `/` редиректит на `/app/`. При отсутствии сборки (dev) — понятная заглушка,
  а не паника. `vite.config.ts`: `base: '/app/'`, dev-прокси `/api` и `/auth`
  на бэкенд.
- `make gen` включает `pnpm gen`; `make build` собирает web, затем Go.

КП:
- `make build` даёт один бинарник, `/app/` открывает страницу, которая
  показывает версию с бэкенда.
- `pnpm typecheck`, `pnpm lint`, `pnpm test` зелёные и входят в `make check`.

### ✅ M0-6 — Docker и CI
Что сделать:
- `deploy/Dockerfile`: multi-stage (node → go → debian-slim; `CGO_ENABLED=1`
  нужен для Matrix crypto/sqlite).
- `deploy/docker-compose.yml`: `postgres:17` + приложение, healthcheck.
- `.github/workflows/ci.yml`: Go 1.26.x, pnpm, `make check`.
- Корневой `README.md`: что это, как запустить локально, ссылки на docs.

КП:
- `docker compose -f deploy/docker-compose.yml up --build` поднимает сервис,
  `/readyz` отвечает 200.

---

## M1 — Идентичность и сессии

### ✅ M1-1 — Пользователи и PG-хранилище привязок
- Таблица `messenger_bindings` (`user_id`, `platform`, `remote_user_id`,
  `conversation_id`, `verified_at`, `metadata jsonb`; unique
  `(platform, remote_user_id)`).
- `internal/auth`: `UserService.UpsertFromOIDC(issuer, subject, email, name)`.
- Реализация `unibot.IdentityStore` на PG; `PrincipalID` = `users.id`.
- `oidcflow`: вместо хеша issuer+subject принимает колбэк
  `ResolvePrincipal(ctx, claims) (PrincipalID, error)`.

КП: интеграционные тесты Bind/Resolve/Bindings; перепривязка мессенджера к
другому пользователю переносит привязку, а не дублирует.

### ✅ M1-2 — PG-хранилище действий (InteractionStore)
- Таблица `interactions` (токен или ключ сообщения → действие, `expires_at`).
- Реализация `unibot.InteractionStore`; фоновая очистка просроченных.

КП: действие переживает рестарт процесса (интеграционный тест); просроченное
не разрешается.

### ✅ M1-3 — Сессии и middleware авторизации
- Таблица `sessions` (`token_hash`, `user_id`, `kind`, `expires_at`,
  `last_used_at`). Токен — 32 случайных байта, в БД только sha256.
- В `openapi.yaml`: security schemes `bearerAuth` и `cookieAuth`;
  `GET /me` → профиль и список привязок; `PATCH /me` (timezone);
  `POST /auth/logout`.
- Middleware кладёт `PrincipalID` в контекст; операции без сессии → 401.
- Для cookie-сессий изменяющие запросы требуют `Content-Type: application/json`
  (защита от CSRF вместе с `SameSite=Lax`).

КП: тесты 401/200, истёкшая сессия, logout отзывает токен.

### ✅ M1-4 — Безопасная привязка мессенджера через OIDC
- Состояние ожидающего входа — в PG (`auth_pending`), TTL 10 минут.
- После OIDC-callback — страница подтверждения с названием платформы,
  идентификатором мессенджера и именем SSO-аккаунта; привязка создаётся только
  по POST с одноразовым токеном.
- Для Telegram: страница `/auth/login` загружает `telegram-web-app.js`,
  отправляет `initData`; сервер проверяет подпись и совпадение `user.id` с
  ожидающей привязкой; при несовпадении — отказ.
- После привязки: сообщение в чат «✅ Аккаунт привязан» + кнопка «Открыть
  приложение».
- Вход из браузера без привязки: `/auth/login?return_to=/app/` → cookie-сессия.

КП: тесты — чужой `initData` отклоняется; привязка без подтверждения не
создаётся; повторное использование state/токена невозможно; `return_to`
принимает только относительные пути.

### ✅ M1-5 — Сессия Telegram Mini App
- `POST /auth/telegram {init_data}` → `{status: "ok", token, expires_at, user}`
  либо `{status: "link_required", link_url}`.
- Проверка `initData`: HMAC токеном бота (есть в `unibot/webapp`), `auth_date`
  не старше 24 часов.

КП: тесты на валидный, просроченный, поддельный `initData` и на
непривязанного пользователя.

---

## M2 — Доработки unibot

### ✅ M2-1 — HTML-форматирование сообщений
`Message.HTML` (подмножество: `b`, `i`, `code`, `a`); Telegram —
`parse_mode=HTML`, Matrix — `formatted_body`; `Text` остаётся plain-фолбэком.
Хелпер экранирования. КП: тесты рендера обоих адаптеров.

### ✅ M2-2 — Ответ на нажатие действия
`botctx.Ack(ctx, text, alert)`; Telegram — `answerCallbackQuery` с текстом
(адаптер больше не отвечает сразу; если обработчик не вызвал `Ack`, runtime
отправляет пустой ответ); Matrix — no-op. КП: тест с фейковым Bot API.

### ✅ M2-3 — TTL и адресат действия
`Action.TTL` (по умолчанию 24 ч) и возможность привязать действие к
`PrincipalID`, а не только к remote user. КП: тесты.

### ✅ M2-4 — Надёжность runtime
Логирование ошибок обработчика и `Resolve`, recover паник, таймаут обработчика,
обработка событий одного пользователя по порядку. КП: тесты.

### ✅ M2-5 — Только личные чаты, команды, кнопка меню
Telegram: игнор не-private чатов; разбор команд (`/start`, `/help`);
`setChatMenuButton` на WebApp и `setMyCommands` при старте.
`internal/bot.Router`: команды, префиксы действий, обработчик «не привязан»
(кнопка «Войти»), ответ по умолчанию. КП: тесты роутера.

---

## M3 — Напоминания (бэкенд)

### ✅ M3-1 — Схема и расчёт расписания
- Таблицы `reminders` (`id`, `user_id`, `message` 1–1500, `timezone`,
  `schedule_kind` weekly|monthly, `schedule_days smallint[]`, `time_hhmm`,
  `repeat_minutes` 1–1440, `enabled`, `next_due_at`, `last_error`,
  `lock_until`, timestamps) и `reminder_occurrences` (`id`, `reminder_id`,
  `scheduled_at`, `first_sent_at`, `repeat_count`, `next_repeat_at`,
  `status` active|completed|cancelled, `completed_at`, `deliveries jsonb`);
  частичный unique-индекс: одно активное срабатывание на напоминание.
- `schedule.NextDue(after, kind, days, hhmm, tz) time.Time` — чистая функция.

КП: табличные тесты — недели, месяцы, 31 → последний день месяца, переход
года, DST (несуществующее и повторяющееся локальное время), «сегодня, но время
уже прошло».

### ✅ M3-2 — Сервис и API CRUD
Операции в `openapi.yaml` (тег `reminders`): list, create, get, update, delete,
`POST /reminders/{id}/complete`. Сервис проверяет владельца; правка/выключение
отменяет активное срабатывание и пересчитывает `next_due_at`.
КП: тесты API, включая доступ к чужому напоминанию (404).

### ✅ M3-3 — Планировщик
Job с тикером 15 с: выбрать должные (`next_due_at <= now` или активное
срабатывание с `next_repeat_at <= now`) c `FOR UPDATE SKIP LOCKED`, аренда 55 с,
отправка через `SendToPrincipal`, запись результата. Повтор: новое сообщение,
старое удаляется. Ошибка отправки → `last_error`, аренда снимается, повтор на
следующем тике. Текст — как в n8n: сообщение, «Первое напоминание: <b>дата</b>»,
«Количество повторов: <b>N</b>».
КП: тесты с фейковым транспортом и управляемыми часами — первое срабатывание,
повтор, догон после простоя, два параллельных планировщика не дублируют
отправку.

### ✅ M3-4 — Подтверждение из чата
Действие `reminders.done:<reminder_id>:<occurrence_id>`: проверка владельца и
актуальности, ответы «Подтверждено ✅» / «Уже подтверждено ✅» / «Это уже
неактуальное напоминание» / «Это напоминание не для вас» (alert) /
«Напоминание уже недоступно»; сообщение заменяется на текст с «✅ Выполнено».
Команда `/reminders` — краткий список + кнопка WebApp.
КП: тесты всех веток.

---

## M4 — WebApp

### ✅ M4-1 — Оболочка и мост к хосту
`web/src/host`: telegram | browser (matrix — заглушка); тема, haptics, alert,
кнопка «назад». Авторизация: Mini App → `/auth/telegram`, браузер → cookie,
экран «Нужно войти». Главный экран со списком фич, маршрутизация.

### ✅ M4-2 — Экран напоминаний
Паритет с Mini App из n8n: список как будильники с переключателем, модальный
редактор (текст, несколько времён, дни недели / дни месяца, повтор, вкл/выкл),
удаление в два касания, оптимистичное переключение. Данные —
`openapi-react-query`. КП: тесты компонентов формы и валидации.

---

## M5 — MCP

### ✅ M5-1 — Персональные токены
Таблица `api_tokens`; API create/list/revoke; экран в WebApp; токен
показывается один раз; Bearer-PAT принимается API и MCP.

Сделано: миграция `00008_api_tokens`; `internal/auth` (формат `cv_pat_`, sha256,
реестр областей `profile:read`/`reminders:read`/`reminders:write`, отзыв,
`last_used_at` раз в минуту, корзина неудачных попыток на IP); тег `tokens` в
OpenAPI; middleware проверяет scope и запрещает PAT управление токенами и
`PATCH /me`; экран `web/src/features/tokens` с одноразовым показом секрета.

### ✅ M5-2 — MCP-сервер и инструменты
`/mcp` (Streamable HTTP), авторизация PAT. Инструменты: `whoami`,
`reminders_list`, `reminders_create`, `reminders_update`, `reminders_delete`,
`reminders_complete` — все через сервис фичи. КП: тест через клиент SDK.

Сделано: `github.com/modelcontextprotocol/go-sdk` v1.8.0, stateless `/mcp`,
PAT-only авторизация, `Principal` в контексте, `Feature.RegisterMCP` для
инструментов фич, `reminders_get` дополнительно к списку; тесты через
in-memory транспорт и реальный Streamable HTTP; `docs/MCP.md` генерируется
командой `cublyverse docs mcp` и проверяется в `make check` вместе с
`scripts/check-docs.sh`.

---

## M6 — Выпуск

### ✅ M6-1 — Импорт из n8n
`cublyverse import-n8n <file>`: экспорт Data Table → таблица ожидания по
Telegram user_id; присвоение при привязке Telegram. Преобразование `days`
(`1,2,3` / `m:19,31`).

Сделано: миграция `00009_legacy_reminders_import`; `reminders.ParseLegacyExport`
(CSV с RFC 4180-кавычками и JSON; строки проходят `canonicalize`, то есть те же
правила, что у сервиса) и `reminders.ImportLegacy` (идемпотентно, `--dry-run`,
невалидные строки с номером и причиной, присвоение уже привязанным сразу);
`reminders.ClaimLegacyReminders` — одна транзакция на аккаунт с
`FOR UPDATE SKIP LOCKED`, вызывается из `auth.ConfirmBinding`/`auth.Bind` после
коммита привязки (best effort: сбой не блокирует вход) и шлёт в чат
«Импортировано напоминаний из старого бота: N». Тесты: парсер (CSV/JSON,
недельные и месячные, `deleted`, кривое время, чужой пояс, многострочные
сообщения, emoji), идемпотентность, dry-run, привязка, уже привязанный
пользователь, гонка четырёх присвоений, CLI на реальном PostgreSQL.

### ✅ M6-2 — Развёртывание и переключение
Документ по деплою, бэкап PG, чек-лист переключения (выключить n8n-воркфлоу,
сменить токен бота, проверить напоминание от начала до конца).

Сделано: боевой `deploy/docker-compose.yml` (два тома, healthcheck, restart
policy, env через `deploy/.env`, приложение только на loopback) и
`deploy/.env.example`; `deploy/backup.sh` (`pg_dump --format=custom`, ретенция);
`docs/DEPLOY.md` (все переменные, первый запуск, OIDC, Telegram, прокси/TLS,
обновление и forward-only миграции, откат, бэкап и проверенное восстановление,
том Matrix, наблюдаемость, безопасность); `docs/MIGRATION_FROM_N8N.md` (чек-лист
переключения, почему n8n выключается до старта нового поллера, альтернатива с
новым токеном, присвоение при входе, откат, уборка).

---

## M7 — Документация

Документация — часть определения «готово» для каждого этапа; эта стори
собирает и проверяет итог. Все документы на русском, примеры команд рабочие.

### ⬜ M7-1 — Полная документация
- `docs/ARCHITECTURE.md` (корневой): слои, поток данных бот/API/MCP, модель
  идентичности, планировщик; актуален после всех этапов.
- `docs/API.md`: как устроен OpenAPI-конвейер, как добавить операцию, как
  получить клиент; ссылка на живую страницу `/api/docs`. В `openapi.yaml` у
  каждой операции и схемы есть `summary`/`description`, у полей — примеры.
- `docs/MCP.md`: адрес `/mcp`, авторизация PAT, **каждый инструмент** с
  описанием, входной и выходной схемой, примером вызова и ошибками; подключение
  из Claude Code / Claude Desktop (пример конфига).
- `docs/DATABASE.md`: схема таблиц, как писать миграции (goose), как добавлять
  запросы (sqlc), правила обратной совместимости, бэкап/восстановление.
- `docs/FEATURES.md` + `docs/ADDING_A_FEATURE.md`: пошаговый рецепт новой
  фичи (миграция → OpenAPI → сервис → бот → MCP → WebApp → тесты → доки) на
  примере напоминаний.
- `docs/DEPLOY.md`: переменные окружения (полная таблица), compose, обновление,
  бэкапы, переключение с n8n.
- `docs/USER_GUIDE.md`: как пользователю войти и пользоваться напоминаниями.
- Корневой `README.md` ведёт на все документы.
- Проверка в `make check`: каждый MCP-инструмент, зарегистрированный в коде,
  упомянут в `docs/MCP.md`; каждая переменная окружения из `internal/config`
  есть в `docs/DEPLOY.md` и `.env.example` (скрипт `scripts/check-docs.sh`).

КП: `make check` зелёный вместе с проверкой документации; по `docs/DEPLOY.md` с
нуля поднимается рабочий стенд.
