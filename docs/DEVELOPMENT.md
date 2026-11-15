# Разработка

Как поднять Cublyverse локально, прогнать проверки и не наступить на типичные
грабли. Боевая установка — в [`DEPLOY.md`](DEPLOY.md), устройство кода — в
[`ARCHITECTURE.md`](ARCHITECTURE.md).

## Что нужно

| Инструмент | Версия | Зачем |
|---|---|---|
| Go | 1.27.4 (см. `go.mod`) | бэкенд, генераторы, тесты |
| Node.js | 24 LTS (подойдёт 22.12+) | WebApp и Vite |
| pnpm | 12.6.0 (`packageManager` в `web/package.json`) | зависимости и сборка WebApp |
| Docker | любая свежая версия | PostgreSQL для разработки и интеграционные тесты |
| PostgreSQL | 17 | альтернатива Docker для локальной базы |

Docker нужен не для запуска сервера, а для базы и интеграционных тестов. Без
Docker `make check` остаётся зелёным: тесты на testcontainers пропускаются.

```bash
go version        # go1.27.4
node --version    # v24.x
pnpm --version    # 12.6.0
docker --version
```

## 1. Клонировать и поставить зависимости

```bash
git clone <repo-url> cublyverse
cd cublyverse
```

`make` сам поставит зависимости WebApp из `web/pnpm-lock.yaml` при первом
запуске любой цели, которой нужен `web/node_modules`. Отдельный `pnpm install`
не обязателен, но полезен, если вы работаете в `web/` вручную.

## 2. Поднять PostgreSQL

Самый предсказуемый способ — одноразовый контейнер. Порт `55432` выбран, чтобы
не конфликтовать с локальным PostgreSQL на `5432`:

```bash
docker run --rm -d --name cublyverse-dev-db \
  -e POSTGRES_USER=cublyverse \
  -e POSTGRES_PASSWORD=cublyverse \
  -e POSTGRES_DB=cublyverse \
  -p 55432:5432 \
  postgres:17

docker exec cublyverse-dev-db pg_isready -U cublyverse -d cublyverse
```

Ожидаемый ответ: `... accepting connections`. Контейнер удалится вместе с
данными после `docker rm -f cublyverse-dev-db`; база пересоздастся при
следующем запуске.

Если PostgreSQL уже установлен локально:

```bash
createdb cublyverse
# DATABASE_URL=postgres://localhost:5432/cublyverse?sslmode=disable
```

## 3. Создать `.env`

```bash
cp .env.example .env
```

Для локального запуска достаточно изменить `DATABASE_URL` (порт из шага 2).
Остальные значения можно оставить пустыми: без OIDC и токенов мессенджеров
сервер обслуживает API, WebApp и MCP, а бот не запускается.

Минимальный `.env`:

```dotenv
DATABASE_URL=postgres://cublyverse:cublyverse@localhost:55432/cublyverse?sslmode=disable
PUBLIC_BASE_URL=http://localhost:8080
LOG_FORMAT=text
```

`PUBLIC_BASE_URL` не обязателен, но с ним ссылки в логах и ответах выглядят
реалистичнее. Полный список переменных и правила валидации — в
[`DEPLOY.md`](DEPLOY.md); шаблон — `.env.example`.

`.env` в git не попадает (`.gitignore`). Секреты в репозиторий не коммитятся.

## 4. Запустить бэкенд

```bash
make dev
```

Цель читает `.env`, запускает `go run ./cmd/cublyverse serve`, применяет
миграции при старте и слушает `http://localhost:8080`. Первый запуск может
занять время: `make` ставит зависимости WebApp, `go run` собирает бинарник.

`make dev` **не собирает WebApp**. `/app/` в этом режиме отдаёт понятную
заглушку «WebApp не собран»; для разработки интерфейса запустите Vite (шаг 7),
а для проверки встроенной сборки — `make build` (шаг 6).

### Проверить, что всё поднялось

В другом терминале:

```bash
# процесс жив (текст, не JSON)
curl -i http://localhost:8080/healthz

# база отвечает
curl -i http://localhost:8080/readyz

# версия API
curl -sS http://localhost:8080/api/v1/health

# страница документации API (Swagger UI с CDN)
curl -I http://localhost:8080/api/docs

# спецификация
curl -I http://localhost:8080/api/openapi.yaml

# WebApp (заглушка или сборка)
curl -I http://localhost:8080/app/

# MCP без токена обязан ответить 401
curl -i -X POST http://localhost:8080/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'
```

Ожидаемо: `/healthz` — `200` с текстом `ok`; `/readyz` — `200` и
`{"status":"ok"}`; `/api/v1/health` — `200` и `{"status":"ok","version":"…"}`;
`/app/` — `200` (заглушка либо `index.html`); `/mcp` без заголовка
`Authorization` — `401` и JSON `{"code":"unauthorized",…}` с
`WWW-Authenticate`.

## 5. Тесты и проверки

```bash
make check        # то, что должно быть зелёным перед коммитом
```

`make check` по порядку запускает:

1. `gen-check` — перегенерирует код и падает, если файлы изменились;
2. `check-docs` — проверяет документацию (см. ниже);
3. `lint` — `go vet`, ESLint, `tsc --noEmit`;
4. `test` — Go-тесты и Vitest.

Отдельные цели:

```bash
make test         # Go + WebApp
make test-go      # только go test ./...
make test-web     # только vitest
make lint         # vet + ESLint + typecheck
make check-docs   # только проверки документации
```

Интеграционные тесты (`internal/store`, `internal/auth`,
`internal/interaction/postgres`, `internal/features/reminders`,
`internal/mcpserver`, `internal/app`) поднимают `postgres:17` через
testcontainers-go. Им нужен работающий Docker; без Docker они пропускаются с
`t.Skip`, поэтому `make check` не падает на машине без контейнеров. Полная
проверка базы и миграций идёт только с Docker.

Точечный прогон:

```bash
go test ./internal/features/reminders/... -run TestScheduler -v
go test ./internal/app/ -run TestMCP -v
cd web && pnpm test -- src/features/reminders
```

## 6. Собрать один бинарник

```bash
make build
```

Собирает `web/dist`, встраивает его в Go-бинарник и кладёт результат в
`bin/cublyverse`. Запуск:

```bash
set -a
. ./.env
set +a
bin/cublyverse serve
```

`bin/cublyverse` (в отличие от `make dev`) не читает `.env` сам: переменные
должны быть в окружении. Миграции применяются при старте, `/app/` отдаёт
настоящий WebApp из `web/dist`. Альтернатива — `make dev`, но тогда WebApp
нужно открывать через Vite.

Другие команды бинарника:

```bash
bin/cublyverse migrate status     # версии миграций и неприменённые файлы
bin/cublyverse migrate up         # применить (serve делает это сам)
bin/cublyverse import-n8n file.csv --dry-run
bin/cublyverse docs mcp --names   # список MCP-инструментов
bin/cublyverse docs api --check   # полнота OpenAPI-документации
```

`migrate` тоже читает `DATABASE_URL` из окружения; `make migrate` заранее
подхватывает `.env`.

## 7. WebApp в режиме разработки

```bash
cd web
pnpm install
pnpm dev
```

Vite поднимает `http://localhost:5173/app/` и проксирует `/api` и `/auth` на
`http://127.0.0.1:8080` (см. `vite.config.ts`). Бэкенд запускается отдельно:

```bash
make dev           # в другом терминале, на :8080
```

Cookie-сессия работает через прокси: `Set-Cookie` относится к
`localhost:5173`, и браузер отправляет cookie дальше. Если OIDC не настроен,
браузерный вход недоступен; экраны можно смотреть в Telegram Mini App или в
тестах с фейковым хостом.

Сборка WebApp:

```bash
cd web
pnpm build         # tsc --noEmit + vite build + заглушка, если dist пуст
pnpm test
pnpm lint
pnpm typecheck
```

## Кодогенерация

```text
api/openapi.yaml ── oapi-codegen ──────► internal/httpapi/gen
                 └─ openapi-typescript ─► web/src/api/schema.d.ts
internal/store/migrations + queries ── sqlc ─► internal/store/db
зарегистрированные MCP-инструменты ── cublyverse docs mcp --write ─► docs/MCP.md
```

```bash
make gen          # всё сразу
make gen-go       # oapi-codegen + sqlc
make gen-web      # openapi-typescript
make gen-docs     # блок MCP.md
make gen-check    # перегенерировать и сравнить (используется в make check)
```

Сгенерированные файлы (`internal/httpapi/gen`, `internal/store/db`,
`web/src/api/schema.d.ts`, блок между маркерами в `docs/MCP.md`) руками не
правятся. Правило простое: меняете `api/openapi.yaml` — сначала `make gen`,
потом код. `make check` падает, если забыли.

## Отладка

- **Логи.** По умолчанию `LOG_FORMAT=text`; для машинного разбора поставьте
  `LOG_FORMAT=json`. Одна строка — одно событие, у HTTP-запросов есть
  `request_id`, `method`, `path`, `status`, `duration_ms`, `remote_addr`.
- **Пробы.** `/healthz` — liveness (процесс жив, база не проверяется);
  `/readyz` — readiness (ходит в базу, `503` при недоступности). Не путайте их.
- **API.** `/api/docs` — Swagger UI (грузится с CDN, нужен интернет);
  `/api/openapi.yaml` — сырая спецификация, работает всегда. Ошибки — единый
  JSON `{"code","message"}`, коды перечислены в [`API.md`](API.md).
- **База.** `bin/cublyverse migrate status`, затем `psql "$DATABASE_URL"`.
  Частые запросы: `select * from reminders order by created_at desc;`,
  `select * from reminder_occurrences where status = 'active';`. Ошибки
  доставки лежат в `reminders.last_error`.
- **Миграции.** Новый файл — `internal/store/migrations/NNNNN_тема.sql` с
  `-- +goose Up/Down`. Подробности — [`DATABASE.md`](DATABASE.md). Не правьте
  применённую миграцию: `serve` накатывает только новые версии.
- **MCP.** `go run ./cmd/cublyverse docs mcp --names` — что зарегистрировано;
  `go run ./cmd/cublyverse docs mcp` — полный справочник. Без PAT `/mcp`
  отвечает `401`, с протухшим — тоже; причина не различается намеренно.
- **Тесты с фейками.** Для тестов без Docker есть управляемые часы
  (`reminders.WithClock`), фейковый `reminders.Delivery` и фейковый хост
  WebApp (`web/src/test/fakeHost.ts`).
- **Документация.** `make check-docs` проверяет ссылки, индекс, миграции,
  make-цели, MCP-инструменты и переменные окружения. Новый документ нужно
  добавить в [`docs/README.md`](README.md).

### Частые проблемы

| Симптом | Причина и решение |
|---|---|
| `missing required environment variables: DATABASE_URL` | не создан `.env` или переменная пуста; `make dev` читает `.env`, `bin/cublyverse` — только окружение. |
| `connect: connection refused` на старте | контейнер базы не поднялся или неверный порт; проверьте `docker exec cublyverse-dev-db pg_isready`. |
| `/readyz` отвечает `503` | база недоступна; смотрите `DATABASE_URL` и логи `app`. |
| `/app/` показывает «WebApp не собран» | это `make dev` без сборки; сделайте `make build` или откройте Vite на `:5173/app/`. |
| `make check` падает на `gen-check` | забыт `make gen`; не правьте сгенерированные файлы. |
| интеграционные тесты пропущены | нет Docker; это ожидаемо, `t.Skip` не ошибка. |
| браузерный вход недоступен | не задан `OIDC_ISSUER`/`OIDC_CLIENT_ID`; локально используйте Mini App или тесты. |
| `409 Conflict` в логах Telegram | второй поллер с тем же токеном; остановите n8n или другую копию. |
| `403` на `PATCH /me` с cookie | клиент не прислал `Content-Type: application/json` (CSRF-правило). |

## Цели Makefile

| Цель | Что делает |
|---|---|
| `all` | синоним `check` (цель по умолчанию). |
| `gen` | `gen-go` + `gen-web` + `gen-docs`. |
| `gen-go` | `oapi-codegen` для Go-сервера и `sqlc generate`. |
| `gen-web` | `openapi-typescript` в `web/src/api/schema.d.ts`. |
| `gen-docs` | переписывает сгенерированный блок `docs/MCP.md`. |
| `gen-check` | запускает генераторы и падает при изменении файлов. |
| `build` | `build-web`, затем бинарник `bin/cublyverse`. |
| `build-web` | `pnpm build` в `web/`. |
| `build-go` | `go build` с версией из `git describe` в `bin/cublyverse`. |
| `test` | `test-go` + `test-web`. |
| `test-go` | `go test ./...` (интеграционные тесты требуют Docker). |
| `test-web` | `pnpm test` (Vitest). |
| `lint` | `lint-go` + `lint-web` + `typecheck`. |
| `lint-go` | синоним `vet`. |
| `vet` | `go vet ./...`. |
| `lint-web` | ESLint в `web/`. |
| `typecheck` | `tsc --noEmit` в `web/`. |
| `check` | `gen-check`, `check-docs`, `lint`, `test` — всё, что нужно перед коммитом. |
| `check-docs` | `scripts/check-docs.sh`. |
| `dev` | `go run ./cmd/cublyverse serve` с окружением из `.env`. |
| `migrate` | `go run ./cmd/cublyverse migrate` (по умолчанию `up`). |
| `clean` | удаляет `bin/` и `web/dist` (оставляя заглушку). |

## Соглашения репозитория

- Работайте по стори из [`STORIES.md`](STORIES.md), отмечайте статус ✅.
- Бизнес-логика — в `internal/features/<feature>`; HTTP, бот и MCP — тонкие
  адаптеры над одним сервисом.
- Любой доступ к данным ограничен `PrincipalID`; «дай всё, отфильтрую в коде»
  запрещено.
- SQL — только через sqlc-запросы; схема — только через новые goose-миграции.
- Go: сначала стандартная библиотека (`net/http`, `log/slog`, `flag`); ошибки
  оборачиваются `%w`; тесты табличные.
- Текст для пользователя (бот, WebApp, ошибки API) — по-русски; код, комментарии,
  коммиты и логи — по-английски.
- Секреты — только в окружении; новые переменные — в `.env.example` и
  [`DEPLOY.md`](DEPLOY.md).
- Документация — часть «готово»: новая операция, миграция, MCP-инструмент,
  переменная или фича обновляют соответствующий документ в том же изменении.

## Чистый клон: минимальная проверка

Если нужно убедиться, что инструкция работает с нуля, повторите шаги 1–4 в
новом каталоге и выполните `make build`. Ожидаемый результат: `/readyz`
отвечает `200`, `/api/v1/health` возвращает версию, `/app/` открывает WebApp,
`/mcp` без токена — `401`. Закончив, удалите контейнер
(`docker rm -f cublyverse-dev-db`) и временный клон.
