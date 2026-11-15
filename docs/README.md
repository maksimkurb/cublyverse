# Документация Cublyverse

Всё о проекте: что он делает, как устроен, как его запустить, развернуть и
расширить. Документы на русском; идентификаторы кода, команд и API — как в
исходниках.

## Навигация

| Документ | О чём |
|---|---|
| [`PLAN.md`](PLAN.md) | План: принятые решения, этапы M0–M7, бэклог и известные ограничения. |
| [`STORIES.md`](STORIES.md) | Стори с критериями приёмки и отчётом о сделанном. |
| [`ARCHITECTURE.md`](ARCHITECTURE.md) | Слои, потоки бот/API/MCP, модель идентичности, планировщик, сборка в `internal/app`. |
| [`FEATURES.md`](FEATURES.md) | Каталог фич; устройство напоминаний: расписание, доставка, подтверждение. |
| [`ADDING_A_FEATURE.md`](ADDING_A_FEATURE.md) | Пошаговый рецепт новой фичи на примере напоминаний: миграция → sqlc → сервис → API → бот → MCP → WebApp → тесты → доки. |
| [`API.md`](API.md) | OpenAPI-конвейер, аутентификация (Bearer/cookie/PAT/scopes), формат ошибок, все группы эндпоинтов. |
| [`MCP.md`](MCP.md) | MCP-эндпоинт `/mcp`, авторизация PAT, все инструменты со схемами и примерами, подключение клиентов. |
| [`DATABASE.md`](DATABASE.md) | Все таблицы и колонки, миграции goose, запросы sqlc, совместимость, бэкап и восстановление. |
| [`WEBAPP.md`](WEBAPP.md) | Устройство WebApp, мост к Telegram/браузеру, вход, маршрутизация, экраны. |
| [`DEPLOY.md`](DEPLOY.md) | Боевая установка: переменные окружения, compose, OIDC, Telegram, прокси, обновление, бэкапы, диагностика. |
| [`DEVELOPMENT.md`](DEVELOPMENT.md) | Локальная разработка: зависимости, `.env`, PostgreSQL в Docker, `make dev`, тесты без Docker, pnpm-сервер, кодогенерация, отладка, все make-цели. |
| [`USER_GUIDE.md`](USER_GUIDE.md) | Инструкция для пользователя: вход через бота, привязка, напоминания в чате и приложении, токены для MCP, FAQ. |
| [`MIGRATION_FROM_N8N.md`](MIGRATION_FROM_N8N.md) | Переключение со старого n8n-воркфлоу «Cubly Reminder»: импорт данных, порядок остановки, откат, уборка. |

Смежные документы вне `docs/`:

- [`../README.md`](../README.md) — короткое описание проекта и быстрый старт.
- [`../AGENTS.md`](../AGENTS.md) — правила для разработчиков и агентов.
- [`../unibot/README.md`](../unibot/README.md) и
  [`../unibot/docs/ARCHITECTURE.md`](../unibot/docs/ARCHITECTURE.md) —
  транспортный фреймворк `unibot`.

## С чего начать

**Пользователю:** [`USER_GUIDE.md`](USER_GUIDE.md) — вход, привязка,
напоминания и токены. Технические детали не нужны.

**Новому разработчику:**

1. [`PLAN.md`](PLAN.md) — зачем проект и какие решения приняты.
2. [`ARCHITECTURE.md`](ARCHITECTURE.md) — как всё собрано.
3. [`FEATURES.md`](FEATURES.md) — что уже есть.
4. [`DEVELOPMENT.md`](DEVELOPMENT.md) — как поднять локально и прогнать тесты.
5. [`DATABASE.md`](DATABASE.md) → [`API.md`](API.md) → [`MCP.md`](MCP.md) →
   [`WEBAPP.md`](WEBAPP.md) — по слоям, в любом порядке после архитектуры.
6. [`ADDING_A_FEATURE.md`](ADDING_A_FEATURE.md) — когда пора добавлять фичу.

**Тем, кто разворачивает:** [`DEPLOY.md`](DEPLOY.md), затем
[`MIGRATION_FROM_N8N.md`](MIGRATION_FROM_N8N.md), если сервис заменяет старый
бот.

## Как документация проверяется

`make check` включает `scripts/check-docs.sh`:

- каждая операция и схема `api/openapi.yaml` задокументированы
  (`cublyverse docs api --check`);
- каждый зарегистрированный MCP-инструмент упомянут в [`MCP.md`](MCP.md);
- каждая переменная окружения из `internal/config` есть в
  [`DEPLOY.md`](DEPLOY.md) и `.env.example`;
- относительные ссылки между Markdown-файлами разрешаются;
- каждый `docs/*.md` перечислен в этом индексе;
- каждый файл миграции перечислен в [`DATABASE.md`](DATABASE.md);
- каждая цель `Makefile` описана в [`DEVELOPMENT.md`](DEVELOPMENT.md).

Поэтому документ, добавленный без ссылки в индексе, или операция без описания
ломают сборку — так же, как забытый `make gen`.
