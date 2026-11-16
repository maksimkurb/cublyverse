# Cublyverse

Персональный бот-ассистент: чат-бот в мессенджерах (Telegram, Matrix), WebApp и
MCP-сервер поверх одной PostgreSQL. Бот всегда работает от имени конкретного
пользователя: вход по SSO (OIDC), аккаунты мессенджеров — проверенные привязки к
SSO-пользователю.

Готовы все этапы M0–M7: один бинарник, OpenAPI-конвейер, PostgreSQL с
миграциями, Docker и CI; вход по SSO с безопасной привязкой мессенджеров и
сессиями (cookie для браузера, Bearer для Telegram Mini App); напоминания целиком
(CRUD-API, сервис и планировщик, подтверждение из чата); WebApp с напоминаниями,
профилем и персональными токенами; MCP-сервер с инструментами напоминаний;
импорт из n8n и полная документация, проверяемая в `make check`.

**Документация:** [оглавление](docs/README.md) — все документы с описаниями и
порядком чтения. Кратко:

- [Обзор архитектуры](docs/ARCHITECTURE.md) и [план](docs/PLAN.md)
- [Разработка](docs/DEVELOPMENT.md) — локальный запуск с нуля
- [API](docs/API.md) · [MCP](docs/MCP.md) · [база](docs/DATABASE.md) ·
  [WebApp](docs/WEBAPP.md)
- [Развёртывание](docs/DEPLOY.md) · [переключение с n8n](docs/MIGRATION_FROM_N8N.md)
- [Фичи](docs/FEATURES.md) и [как добавить фичу](docs/ADDING_A_FEATURE.md)
- [Руководство пользователя](docs/USER_GUIDE.md)
- [Правила разработки](AGENTS.md) · [стори](docs/STORIES.md)

## Что нужно

- Go 1.27.4 (см. `go.mod`)
- Node.js 24 LTS (подойдёт 22.12+) и pnpm 12.6.0
- PostgreSQL 17 — локально или в Docker
- Docker — для PostgreSQL в разработке и интеграционных тестов (не обязателен
  для запуска сервера)

## Быстрый старт

Полная инструкция с пояснениями — [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md).
Минимальный вариант:

```bash
# 1. PostgreSQL в одноразовом контейнере (порт 55432, чтобы не мешать локальному)
docker run --rm -d --name cublyverse-dev-db \
  -e POSTGRES_USER=cublyverse -e POSTGRES_PASSWORD=cublyverse \
  -e POSTGRES_DB=cublyverse -p 55432:5432 postgres:17

# 2. Конфигурация
cp .env.example .env
# в .env: DATABASE_URL=postgres://cublyverse:cublyverse@localhost:55432/cublyverse?sslmode=disable

# 3. Запуск (миграции применяются автоматически)
make dev
```

`make dev` читает `.env`, ставит зависимости WebApp при первом запуске и
поднимает сервер на <http://localhost:8080>. Это режим бэкенда: `/app/` отдаёт
заглушку «WebApp не собран», пока вы не выполните `make build` или не запустите
Vite (`cd web && pnpm dev`, см. ниже).

Проверьте, что всё поднялось:

- <http://localhost:8080/healthz> — процесс жив (текстовый `ok`)
- <http://localhost:8080/readyz> — база отвечает
- <http://localhost:8080/api/v1/health> — версия бэкенда
- <http://localhost:8080/api/docs> — документация API (Swagger UI с CDN)
- <http://localhost:8080/app/> — WebApp (заглушка или сборка)
- `POST /mcp` без токена — `401`

Минимально достаточно одной переменной `DATABASE_URL`: без токенов
мессенджеров сервер поднимается и обслуживает API, WebApp и MCP.

### Собрать один бинарник

```bash
make build          # web/dist + bin/cublyverse
set -a; . ./.env; set +a
bin/cublyverse serve
```

Бинарник не читает `.env` сам — переменные должны быть в окружении. `make dev`
делает это за вас.

### Вход

`GET /api/v1/me` требует сессию. В браузере вход начинается со страницы
<http://localhost:8080/auth/login?return_to=/app/> — для неё нужен
OIDC-провайдер (`OIDC_ISSUER`, `OIDC_CLIENT_ID`). Telegram Mini App входит через
`POST /api/v1/auth/telegram` с `initData`. Как устроены сессии, подтверждение
привязки и проверка `initData` — в [docs/API.md](docs/API.md), для пользователя —
в [docs/USER_GUIDE.md](docs/USER_GUIDE.md).

### Разработка WebApp

```bash
cd web
pnpm install
pnpm dev      # http://localhost:5173/app/, прокси /api и /auth на :8080
```

Бэкенд запускается отдельно (`make dev`). Сборка встраивается в бинарник
(`make build`); подробности — в [docs/WEBAPP.md](docs/WEBAPP.md).

### PostgreSQL без Docker

```bash
createdb cublyverse
export DATABASE_URL='postgres://localhost:5432/cublyverse?sslmode=disable'
```

Миграции применяются автоматически при старте `serve`; вручную —
`bin/cublyverse migrate up` и `bin/cublyverse migrate status` (или `make migrate`).

### Через Docker Compose

```bash
cp deploy/.env.example deploy/.env    # пароль БД, PUBLIC_BASE_URL, OIDC, токен бота
docker compose -f deploy/docker-compose.yml up -d --build
docker compose -f deploy/docker-compose.yml ps
```

Поднимаются PostgreSQL 17 и приложение с healthcheck и томами; готовность
проверяется через `/readyz`. Пример боевой: наружу приложение отдаёт обратный
прокси, переменные и порядок первого запуска — в [docs/DEPLOY.md](docs/DEPLOY.md).

## Команды

```text
make gen        перегенерировать Go-сервер, sqlc, TS-клиент и блок MCP.md
make check      gen без диффа + проверка документации + vet + lint + typecheck + тесты
make build      собрать WebApp, затем бинарник bin/cublyverse
make test       тесты Go и WebApp
make lint       go vet, ESLint, tsc --noEmit
make check-docs только проверки документации
make dev        запустить бэкенд с конфигурацией из .env
make migrate    применить миграции (serve делает это сам)
```

Полный список целей и их назначение — в
[docs/DEVELOPMENT.md](docs/DEVELOPMENT.md#цели-makefile). `make gen` работает на
чистой машине: генераторы закреплены в `go.mod` (директивы `tool`), зависимости
WebApp ставятся из `web/pnpm-lock.yaml`.

## Структура

```text
api/openapi.yaml          спецификация API — источник правды
cmd/cublyverse/           main: serve | migrate | import-n8n | docs
internal/config/          конфигурация из env
internal/store/           pgxpool, миграции goose, sqlc-запросы
internal/auth/            пользователи, привязки, вход OIDC, сессии, PAT
internal/interaction/     реестр действий в PostgreSQL (для бота)
internal/httpapi/         сгенерированный strict-сервер и его реализация
internal/feature/         контракты фичи (Feature, Job)
internal/features/        фичи: бизнес-логика, бот/MCP-адаптеры, фоновые задачи
internal/bot/             роутер команд и действий, общие ответы
internal/mcpserver/       MCP-сервер и генерация справочника инструментов
internal/app/             сборка приложения, реестр фич, фоновые задачи
unibot/                   наш фреймворк мессенджеров
web/                      WebApp (Vite + React + TypeScript + Base UI)
deploy/                   Dockerfile, docker-compose, backup.sh
docs/                     документация (оглавление — docs/README.md)
scripts/                  проверки генерации и документации
```

Слои: бот, HTTP API и MCP — три тонких входа в один сервисный слой фичи, логика
не дублируется. HTTP описан в `api/openapi.yaml`; сгенерированные файлы
(`internal/httpapi/gen`, `internal/store/db`, `web/src/api/schema.d.ts`) правятся
только генераторами.

## Тесты

```bash
make check
```

Интеграционные тесты поднимают `postgres:17` через testcontainers-go. Если Docker
недоступен, они пропускаются (`t.Skip`), поэтому `make check` остаётся зелёным и
без Docker; полная проверка базы и миграций идёт только с ним. Проверки
документации (`scripts/check-docs.sh`) работают без Docker, сети и базы.
