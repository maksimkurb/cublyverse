# База данных

PostgreSQL 17 — единственное хранилище состояния. Доступ к ней идёт только
через `internal/store`: пул `pgx/v5`, запросы из `sqlc`, схема из миграций
`goose`, встроенных в бинарник.

## Миграции

- Файлы: `internal/store/migrations/NNNNN_<тема>.sql`, встроены через `embed`.
- Нумерация сквозная и **не переиспользуется**. Применённую миграцию править
  нельзя: изменение схемы — это новая миграция.
- В каждой миграции есть `-- +goose Up` и `-- +goose Down`. Down нужен для
  локальных экспериментов, в продакшене откат — отдельное осознанное решение.
- `serve` применяет миграции при старте; вручную:

```text
cublyverse migrate up
cublyverse migrate status
```

`make check` не проверяет миграции на БД (для этого нужен Docker), но
интеграционные тесты поднимают `postgres:17` через testcontainers и применяют
их с нуля — это и есть проверка «миграции накатываются на чистую БД».

## Запросы

- SQL живёт в `internal/store/queries/*.sql` с директивами `-- name: Имя :one`
  (`:many`, `:exec`, `:execrows`).
- `make gen` перегенерирует `internal/store/db`; сгенерированный код руками не
  правится.
- Параметры и результаты — типы `sqlc` (`pgtype.UUID`, `pgtype.Timestamptz`,
  `[]byte` для `bytea`). Идентификаторы форматируются в строку и обратно
  хелперами `internal/auth` (`formatUUID`, `parseUUID`).
- Несколько изменений в одной транзакции — `store.Store.InTx(func(q *db.Queries) error)`.
- Каждый запрос данных ограничен владельцем (`user_id` / `PrincipalID`). Нет
  запроса «дай всё», который потом фильтруется в коде.

## Таблицы

### `users`

Пользователь SSO. Источник истины об аккаунте.

| Колонка | Тип | Смысл |
|---|---|---|
| `id` | `uuid` | `PrincipalID` всей системы (бот, API, MCP) |
| `oidc_issuer` | `text` | issuer провайдера |
| `oidc_subject` | `text` | subject провайдера |
| `email`, `display_name` | `text` | последние полученные claims (могут быть `NULL`) |
| `timezone` | `text` | IANA-имя, по умолчанию `UTC` |
| `created_at`, `updated_at` | `timestamptz` | |

Ограничение `UNIQUE (oidc_issuer, oidc_subject)` — ключ пользователя. Второй
аккаунт SSO не может «занять» существующего пользователя.

### `messenger_bindings`

Проверенная привязка аккаунта мессенджера к пользователю.

| Колонка | Тип | Смысл |
|---|---|---|
| `id` | `uuid` | |
| `user_id` | `uuid` → `users` | владелец |
| `platform` | `text` | `telegram` или `matrix` |
| `remote_user_id` | `text` | id внутри мессенджера |
| `conversation_id` | `text` | чат для доставки (у Telegram равен `remote_user_id`) |
| `verified_at` | `timestamptz` | когда пользователь подтвердил привязку |
| `metadata` | `jsonb` | запас для платформенных деталей |
| `created_at`, `updated_at` | `timestamptz` | |

`UNIQUE (platform, remote_user_id)`: аккаунт мессенджера принадлежит ровно
одному пользователю. Перепривязка к другому SSO-аккаунту **переносит** строку
(`ON CONFLICT … DO UPDATE SET user_id = …`), а не создаёт вторую.

### `interactions`

Реестр действий и выборов, которые бот показал в чате.

| Колонка | Тип | Смысл |
|---|---|---|
| `id` | `uuid` | |
| `token_hash` | `bytea` | sha256 непрозрачного токена (Telegram `callback_data`) |
| `platform`, `conversation_id`, `message_id`, `action_key` | `text` | ключ сообщения (Matrix: событие + эмодзи) |
| `action_id`, `choice_id`, `option_id` | `text` | смысл действия |
| `expected_user_id` | `text` | кто вправе нажать |
| `expires_at` | `timestamptz` | TTL действия |
| `created_at` | `timestamptz` | |

Две формы адресации: по токену или по ключу сообщения. Уникальные индексы
`UNIQUE (token_hash)` и `UNIQUE (platform, conversation_id, message_id,
action_key)` не мешают друг другу, потому что в PostgreSQL `NULL` считается
различным: у строки-токена ключ сообщения пуст, у строки-сообщения пуст
`token_hash`. Токен хранится только как sha256.

### `sessions`

Сессии WebApp и браузера.

| Колонка | Тип | Смысл |
|---|---|---|
| `id` | `uuid` | |
| `token_hash` | `bytea` | sha256 токена, сам токен не хранится |
| `user_id` | `uuid` → `users` | владелец |
| `kind` | `text` | `bearer` (Mini App) или `cookie` (браузер) |
| `expires_at` | `timestamptz` | срок жизни |
| `last_used_at` | `timestamptz` | обновляется не чаще раза в минуту |
| `revoked_at` | `timestamptz` | выставлено `logout` |
| `created_at` | `timestamptz` | |

Проверка сессии — один запрос: `revoked_at IS NULL AND expires_at > now()`.
Поэтому отзыв действует мгновенно и на всех репликах.

### `auth_pending`

Незавершённые входы: ожидание OIDC, страница подтверждения привязки.

| Колонка | Тип | Смысл |
|---|---|---|
| `id` | `uuid` | |
| `state_hash` | `bytea` | sha256 state из URL |
| `status` | `text` | `pending_oidc` → `awaiting_confirmation` → `completed` |
| `flow` | `text` | `bind` (привязка мессенджера) или `browser` (вход в браузере) |
| `nonce` | `text` | OIDC nonce |
| `verifier` | `text` | PKCE code verifier (нужен провайдеру, удаляется вместе со строкой) |
| `platform`, `remote_user_id`, `conversation_id` | `text` | что привязываем |
| `return_to` | `text` | относительный путь возврата в браузере |
| `user_id` | `uuid` → `users` | SSO-аккаунт, прошедший OIDC |
| `confirm_token_hash` | `bytea` | sha256 одноразового токена подтверждения |
| `init_data_verified_at` | `timestamptz` | когда `initData` Telegram совпал с привязкой |
| `expires_at` | `timestamptz` | 10 минут от создания (или от перехода к подтверждению) |

Строка создаётся ботом или страницей входа и живёт до завершения либо
истечения срока; просроченные удаляет фоновый job `auth.cleanup`.

## Правила обратной совместимости

1. Только добавляющие изменения в одном релизе: новая таблица, новая
   nullable-колонка, новый индекс `CREATE INDEX CONCURRENTLY` для больших
   таблиц.
2. Удаление и переименование — двумя шагами: сначала код перестаёт
   использовать колонку, в следующем релизе она удаляется.
3. `NOT NULL` добавляется с `DEFAULT` или после заполнения данных.
4. Ограничения (`UNIQUE`, `CHECK`, внешние ключи) добавляются отдельной
   миграцией, чтобы проще было откатить.
5. Любая миграция должна быть безопасна при повторном запуске `serve`: goose
   применяет только новые версии, но код старта не должен зависеть от
   «свежести» схемы.

## Бэкап и восстановление

```bash
# дамп
pg_dump --format=custom --no-owner --dbname "$DATABASE_URL" > cublyverse.dump

# восстановление в чистую базу
createdb cublyverse_restore
pg_restore --no-owner --dbname cublyverse_restore cublyverse.dump
cublyverse migrate status
```

В `deploy/docker-compose.yml` данные лежат в томе `pgdata`. Бэкап тома — это
бэкап базы, но консистентный дамп надёжнее: перед `pg_dump` не нужен останов
сервиса, а `pg_restore` даёт предсказуемую схему.

Секретов в базе нет: токены сессий, state и confirm-токены хранятся как sha256,
`initData` не сохраняется. PKCE verifier — единственный краткоживущий секрет
(до 10 минут), он удаляется вместе со строкой `auth_pending`.

## Тесты

- Юнит-тесты не требуют базы.
- Интеграционные тесты (`internal/store`, `internal/auth`,
  `internal/interaction/postgres`) поднимают `postgres:17` через
  testcontainers. Нужен Docker; без него тесты пропускаются с `t.Skip`.
- `go test ./...` — полный прогон, `make check` — то же плюс линтеры и
  проверки генерации.
