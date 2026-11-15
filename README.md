# Cublyverse

Персональный бот-ассистент: чат-бот в мессенджерах (Telegram, Matrix), WebApp и
MCP-сервер поверх одной PostgreSQL. Бот всегда работает от имени конкретного
пользователя: вход по SSO (OIDC), аккаунты мессенджеров — проверенные привязки к
SSO-пользователю.

Сейчас готовы M0–M4: собирается один бинарник, работает OpenAPI-конвейер,
подключена PostgreSQL с миграциями, есть Docker и CI. Вход — SSO с безопасной
привязкой мессенджеров и сессиями (cookie для браузера, Bearer для Telegram Mini
App). Напоминания работают целиком: CRUD-API, сервис и планировщик, подтверждение
из чата. WebApp повторяет Mini App из n8n: список-будильник, переключатель,
модальный редактор с несколькими временами и днями, профиль с часовым поясом.

- Обзор архитектуры — [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)
- План и принятые решения — [docs/PLAN.md](docs/PLAN.md)
- Стори с критериями приёмки — [docs/STORIES.md](docs/STORIES.md)
- Фичи и их устройство — [docs/FEATURES.md](docs/FEATURES.md)
- API и вход — [docs/API.md](docs/API.md)
- MCP: инструменты, авторизация токенами, подключение клиентов — [docs/MCP.md](docs/MCP.md)
- WebApp: устройство, вход, мост к хосту, добавление экрана — [docs/WEBAPP.md](docs/WEBAPP.md)
- Схема базы и миграции — [docs/DATABASE.md](docs/DATABASE.md)
- Развёртывание — [docs/DEPLOY.md](docs/DEPLOY.md)
- Переключение со старого n8n-бота — [docs/MIGRATION_FROM_N8N.md](docs/MIGRATION_FROM_N8N.md)
- Правила разработки — [AGENTS.md](AGENTS.md)

## Что нужно

- Go 1.27+
- Node.js 22+ и pnpm 12
- PostgreSQL 17 (для локального запуска) либо Docker

## Быстрый старт

```bash
cp .env.example .env      # заполните DATABASE_URL
make dev                  # сервер на http://localhost:8080
```

Проверьте, что всё поднялось:

- <http://localhost:8080/healthz> — процесс жив
- <http://localhost:8080/readyz> — база отвечает
- <http://localhost:8080/api/v1/health> — версия бэкенда
- <http://localhost:8080/api/docs> — документация API
- <http://localhost:8080/app/> — WebApp

Минимально достаточно одной переменной `DATABASE_URL`: без токенов
мессенджеров сервер поднимается и обслуживает API и WebApp.

### Вход

`GET /api/v1/me` требует сессию. В браузере вход начинается со страницы
<http://localhost:8080/auth/login?return_to=/app/> — для неё нужен
OIDC-провайдер (`OIDC_ISSUER`, `OIDC_CLIENT_ID`). Telegram Mini App входит через
`POST /api/v1/auth/telegram` с `initData`. Как устроены сессии, подтверждение
привязки и проверка `initData` — в [docs/API.md](docs/API.md).

### Разработка WebApp

```bash
cd web
pnpm install
pnpm dev      # http://localhost:5173/app/, прокси /api и /auth на :8080
```

Сборка встраивается в бинарник (`make build`); подробности — в
[docs/WEBAPP.md](docs/WEBAPP.md).

### PostgreSQL без Docker

```bash
createdb cublyverse
export DATABASE_URL='postgres://localhost:5432/cublyverse?sslmode=disable'
```

Миграции применяются автоматически при старте `serve`; вручную —
`cublyverse migrate up` и `cublyverse migrate status`.

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
make gen        перегенерировать Go-сервер, sqlc, TS-клиент
make check      gen без диффа + vet + lint + typecheck + тесты
make build      собрать WebApp, затем бинарник bin/cublyverse
make test       тесты Go и WebApp
make lint       go vet, ESLint, tsc --noEmit
make dev        запустить бэкенд с конфигурацией из .env
make migrate    применить миграции (serve делает это сам)
```

`make gen` работает на чистой машине: генераторы закреплены в `go.mod`
(директивы `tool`), зависимости WebApp ставятся из `web/pnpm-lock.yaml`.

## Структура

```text
api/openapi.yaml          спецификация API — источник правды
cmd/cublyverse/           main: serve | migrate | import-n8n | docs
internal/config/          конфигурация из env
internal/store/           pgxpool, миграции goose, sqlc-запросы
internal/auth/            пользователи, привязки, вход OIDC, сессии
internal/interaction/     реестр действий в PostgreSQL (для бота)
internal/httpapi/         сгенерированный strict-сервер и его реализация
internal/feature/         контракты фичи (Feature, Job)
internal/features/        фичи: бизнес-логика, бот/MCP-адаптеры, фоновые задачи
internal/app/             сборка приложения, реестр фич, фоновые задачи
internal/bot/             обработчик бота (роутер — в M2)
internal/mcpserver/       MCP-сервер (наполняется в M5)
unibot/                   наш фреймворк мессенджеров
web/                      WebApp (Vite + React + TypeScript + telegram-ui)
deploy/                   Dockerfile, docker-compose
docs/                     архитектура, план, стори, API, база, деплой
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
без Docker; полная проверка базы идёт только с ним.
