# Cublyverse

Персональный бот-ассистент: чат-бот в мессенджерах (Telegram, Matrix), WebApp и
MCP-сервер поверх одной PostgreSQL. Бот всегда работает от имени конкретного
пользователя: вход по SSO (OIDC), аккаунты мессенджеров — проверенные привязки к
SSO-пользователю.

Сейчас в репозитории каркас (этап M0): собирается бинарник, работает OpenAPI
конвейер, подключена PostgreSQL с миграциями, есть заготовка WebApp, Docker и CI.
Фича «Напоминания» появится на этапе M3.

- Архитектура и принятые решения — [docs/PLAN.md](docs/PLAN.md)
- Стори с критериями приёмки — [docs/STORIES.md](docs/STORIES.md)
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

### PostgreSQL без Docker

```bash
createdb cublyverse
export DATABASE_URL='postgres://localhost:5432/cublyverse?sslmode=disable'
```

Миграции применяются автоматически при старте `serve`; вручную —
`cublyverse migrate up` и `cublyverse migrate status`.

### Через Docker Compose

```bash
docker compose -f deploy/docker-compose.yml up --build
```

Поднимаются PostgreSQL 17 и приложение; готовность проверяется через `/readyz`.

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
cmd/cublyverse/           main: serve | migrate
internal/config/          конфигурация из env
internal/store/           pgxpool, миграции goose, sqlc-запросы
internal/httpapi/         сгенерированный strict-сервер и его реализация
internal/app/             сборка приложения, реестр фич, фоновые задачи
internal/bot/             роутер бота (наполняется в M2)
internal/mcpserver/       MCP-сервер (наполняется в M5)
unibot/                   наш фреймворк мессенджеров
web/                      WebApp (Vite + React + TypeScript)
deploy/                   Dockerfile, docker-compose
docs/                     план и стори
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
