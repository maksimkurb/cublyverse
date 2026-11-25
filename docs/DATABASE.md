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

Отдельной команды «проверить миграции» в `make check` нет, но интеграционные
тесты поднимают `postgres:17` через testcontainers и применяют миграции с нуля
— это и есть проверка «миграции накатываются на чистую БД». Без Docker эти
тесты пропускаются, и такая проверка не выполняется.

### Список миграций

| Версия | Файл | Что создаёт |
|---|---|---|
| 00001 | `00001_init.sql` | `users` — SSO-пользователи и `PrincipalID`. |
| 00002 | `00002_messenger_bindings.sql` | `messenger_bindings` — проверенные привязки Telegram/Matrix. |
| 00003 | `00003_interactions.sql` | `interactions` — реестр действий и выборов бота. |
| 00004 | `00004_sessions.sql` | `sessions` — Bearer- и cookie-сессии. |
| 00005 | `00005_auth_pending.sql` | `auth_pending` — незавершённые входы и подтверждение привязки. |
| 00006 | `00006_interaction_principal_payload.sql` | `principal_id` и `payload` у `interactions`. |
| 00007 | `00007_reminders.sql` | `reminders` и `reminder_occurrences`, индексы и `CHECK`-и. |
| 00008 | `00008_api_tokens.sql` | `api_tokens` — персональные токены. |
| 00009 | `00009_legacy_reminders_import.sql` | `legacy_reminders_import` — комната ожидания импорта n8n. |
| 00010 | `00010_workspaces.sql` | `workspaces`, `workspace_members` — общие пространства без ролей: участник имеет полный доступ. |
| 00011 | `00011_craft.sql` | «Бусинки»: `craft_manufacturers`, `craft_categories` (13 типов материалов), `craft_catalog_items` (глобальные и частные позиции каталога), `craft_inventory` и `craft_inventory_log` (остатки workspace и их история), `craft_projects`, `craft_project_materials`, `craft_wishlist_items`, `craft_wishlist_links`. |
| 00012 | `00012_oauth.sql` | `oauth_clients`, `oauth_codes`, `oauth_tokens` — собственный OAuth 2.1 сервер для MCP: клиенты (DCR и CIMD), одноразовые коды с PKCE, пары access/refresh с ротацией. |
| 00013 | `00013_catalog_sync.sql` | `craft_catalog_sync_runs` — журнал запусков скраперов глобального каталога: источник, статус `running`/`ok`/`failed`, счётчики `created`/`updated`/`unchanged`, текст ошибки. |
| 00014 | `00014_reminders_recurrence.sql` | Гибкое повторение напоминаний: `interval_count`, `month_week_position`, `month_weekday`, `start_date`, `end_kind`, `end_date`, `end_count`, `occurrences_fired`, `finished_at`; новые `CHECK` и бэкфилл старых строк под прежнее поведение. |
| 00015 | `00015_spaces.sql` | Пространства по мини-аппам (`workspaces.app`, `personal`); напоминания получают `workspace_id` (существующие переезжают в личное пространство автора) и получателей `reminder_recipients`; у OAuth убрана привязка к пространству, гранты удалённых адресов `/mcp/w/…` удаляются. |
| 00016 | `00016_craft_reference.sql` | Настраиваемый склад: `craft_categories` и `craft_manufacturers` получают `workspace_id` (строки без него — эталон официальных каталогов), у категорий — свои поля `fields` и связь `catalog_slug`; новые `craft_units` и `craft_space_catalogs`. Существующие склады получают копии категорий, все единицы и оба каталога. |

Нумерация не переиспользуется: следующая миграция — `00017_<тема>.sql`.
`make check` сверяет, что каждый файл из `internal/store/migrations/*.sql`
упомянут в этом документе.

### Как написать миграцию

1. Создайте `internal/store/migrations/NNNNN_<тема>.sql` со следующим номером.
2. Добавьте `-- +goose Up`, изменения и `-- +goose Down` с обратными
   действиями; многострочные операторы оборачивайте
   `-- +goose StatementBegin` / `-- +goose StatementEnd`.
3. Примените локально: `make migrate` (или `bin/cublyverse migrate up`);
   `serve` делает это сам при старте.
4. Проверьте откат на одноразовой базе: `goose` умеет `down`, но в продакшене
   откат схемы — это восстановление дампа, а не Down-миграция.
5. Любая миграция должна безопасно применяться к непустой базе: новые таблицы,
   nullable-колонки или `NOT NULL` с `DEFAULT`. Долгие индексы —
   `CREATE INDEX CONCURRENTLY` с `-- +goose NO TRANSACTION`.

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

### Как добавить запрос

1. Допишите запрос в тематический файл `internal/store/queries/*.sql`:

   ```sql
   -- name: GetReminder :one
   SELECT * FROM reminders
   WHERE id = $1
     AND user_id = $2;
   ```

   `:one` — ровно одна строка, `:many` — список, `:exec` — без результата,
   `:execrows` — количество изменённых строк.

2. `make gen-go` (или `make gen`) — sqlc перегенерирует
   `internal/store/db/*.sql.go`.
3. Вызовите метод из сервиса: `q.GetReminder(ctx, db.GetReminderParams{...})`.
4. Если нужна транзакция, используйте `store.InTx(func(q *db.Queries) error)`.
5. Сгенерированный пакет не правьте: изменение SQL — это изменение
   `queries/*.sql`, а не Go-файла.

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
| `expected_user_id` | `text` | какой аккаунт мессенджера вправе нажать |
| `principal_id` | `uuid` → `users` | необязательное ограничение на SSO-аккаунт |
| `payload` | `text` | непрозрачные данные действия (до 512 байт), возвращаются в событие |
| `expires_at` | `timestamptz` | TTL действия (по умолчанию 24 часа) |
| `created_at` | `timestamptz` | |

Две формы адресации: по токену или по ключу сообщения. Уникальные индексы
`UNIQUE (token_hash)` и `UNIQUE (platform, conversation_id, message_id,
action_key)` не мешают друг другу, потому что в PostgreSQL `NULL` считается
различным: у строки-токена ключ сообщения пуст, у строки-сообщения пуст
`token_hash`. Токен хранится только как sha256.

`expected_user_id` проверяет адаптер (id внутри мессенджера), `principal_id` —
рантайм после `Resolve` (SSO-аккаунт). Второе переживает перепривязку
мессенджера и не даёт подтвердить чужое напоминание. `payload` нужен потому,
что Telegram ограничивает `callback_data` 64 байтами: идентификаторы
напоминания и срабатывания хранятся здесь и приходят в `ActionEvent.Payload`.
Внешний ключ на `users` удаляет действия пользователя вместе с аккаунтом.

```sql
-- 00006_interaction_principal_payload.sql
ALTER TABLE interactions
    ADD COLUMN principal_id uuid REFERENCES users (id) ON DELETE CASCADE,
    ADD COLUMN payload text NOT NULL DEFAULT '';
```

Миграция только добавляющая: существующие строки получают `principal_id = NULL`
(без ограничения) и пустой payload.

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

### `api_tokens`

Персональные токены (PAT) для MCP, скриптов и автоматизации. Токен — это
`cv_pat_` + 32 случайных байта в URL-safe base64; в базе лежит только sha256.

| Колонка | Тип | Смысл |
|---|---|---|
| `id` | `uuid` | идентификатор для отзыва |
| `user_id` | `uuid` → `users` | владелец, `ON DELETE CASCADE` |
| `name` | `text` | название 1..80 символов, видно в WebApp |
| `token_prefix` | `text` | первые 8 символов после маркера (`cv_pat_<prefix>…`) |
| `token_hash` | `bytea` | sha256 токена, `UNIQUE`; plaintext не хранится |
| `scopes` | `text[]` | выданные области доступа (`profile:read`, `reminders:read`, `reminders:write`) |
| `expires_at` | `timestamptz` | срок или `NULL` (бессрочно) |
| `last_used_at` | `timestamptz` | обновляется не чаще раза в минуту |
| `revoked_at` | `timestamptz` | выставлено отзывом; строка остаётся как история |
| `created_at` | `timestamptz` | |

Проверка токена — один запрос по `token_hash`:

```sql
WHERE token_hash = $1
  AND revoked_at IS NULL
  AND (expires_at IS NULL OR expires_at > now())
```

Поэтому отзыв и истечение срока действуют мгновенно на всех репликах.
`UNIQUE (token_hash)` делает коллизии невозможными, а поиск по хешу — это
константное по времени сравнение на уровне индекса: сам секрет нигде не
сравнивается побайтово.

`scopes` намеренно хранится строками: реестр scope-ов живёт в коде
(`internal/auth.Scopes`), и новая область не требует миграции. Ограничения
`CHECK` следят за непустым названием и хотя бы одной областью.

`token_prefix` — не секрет, а метка: по ней пользователь узнаёт строку в
WebApp. Реестр `api_tokens` чистится фоновой задачей `auth.cleanup`: отозванные
строки удаляются через 30 дней (`DeleteRevokedAPITokens`), просроченные
остаются видимыми, пока не отозваны.

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
| `created_at`, `updated_at` | `timestamptz` | |

Строка создаётся ботом или страницей входа и живёт до завершения либо
истечения срока; просроченные удаляет фоновый job `auth.cleanup`.

### `reminders`

Правило напоминания: что сказать, когда и как часто повторять. Владелец —
`users.id`, а не аккаунт мессенджера, поэтому напоминание переживает
перепривязку Telegram или Matrix. Семантика расписания и переходов состояния —
в [FEATURES.md](FEATURES.md).

| Колонка | Тип | Смысл |
|---|---|---|
| `id` | `uuid` | |
| `user_id` | `uuid` → `users` | владелец, `ON DELETE CASCADE` |
| `message` | `text` | текст напоминания, 1..1500 символов |
| `timezone` | `text` | IANA-имя, в котором трактуется время |
| `schedule_kind` | `text` | `once`, `daily`, `weekly`, `monthly` или `yearly` |
| `schedule_days` | `smallint[]` | 1..7 для `weekly`, 1..31 для `monthly` по числам; пусто у остальных видов |
| `interval_count` | `integer` | каждые N дней/недель/месяцев/лет, 1..1000; отсчёт от `start_date` |
| `month_week_position` | `smallint` | для месячного правила «n-й день недели»: 1..4 или `-1` (последний) |
| `month_weekday` | `smallint` | день недели для `month_week_position`, 1..7 |
| `start_date` | `date` | первый возможный день в `timezone`; дата разового напоминания |
| `end_kind` | `text` | `none`, `until` (по `end_date`) или `count` (по `end_count`) |
| `end_date` | `date` | последний разрешённый день для `until` |
| `end_count` | `integer` | всего срабатываний для `count` |
| `occurrences_fired` | `integer` | сколько срабатываний уже было; сбрасывается при правке |
| `finished_at` | `timestamptz` | когда подтвердили последнее срабатывание конечного правила |
| `time_hhmm` | `text` | локальное время `HH:MM`, 24 часа |
| `repeat_minutes` | `integer` | интервал повтора, 1..1440 |
| `enabled` | `boolean` | выключенное напоминание не планируется |
| `next_due_at` | `timestamptz` | ближайшее срабатывание в UTC; `NULL` у выключенного, завершённого или исчерпанного |
| `last_error` | `text` | последняя ошибка доставки, очищается после успешной отправки |
| `lock_until` | `timestamptz` | аренда планировщика (M3-3) |
| `created_at`, `updated_at` | `timestamptz` | |

Ограничения (`CHECK`) дублируют правила сервиса, чтобы некорректная строка не
могла появиться в обход кода: длина сообщения считается в символах
(`char_length`), `time_hhmm` соответствует регулярному выражению
`^([01][0-9]|2[0-3]):[0-5][0-9]$`, `repeat_minutes` в диапазоне 1..1440,
`interval_count` — 1..1000, `schedule_kind` — одно из пяти значений.
`schedule_days` непуст и является подмножеством 1..7 для `weekly` и 1..31 для
месячного правила по числам; у `monthly` с `month_week_position` дни пусты, а
позиция и день недели заполнены парой. `end_kind` согласован с `end_date` и
`end_count`, `occurrences_fired` неотрицателен.

Индексы:

- `reminders_due_idx` — частичный по `next_due_at` (`WHERE enabled AND
  next_due_at IS NOT NULL`): планировщик выбирает только должные включённые
  напоминания;
- `reminders_user_idx` — `(user_id, created_at)` для списка пользователя.

### `reminder_occurrences`

История срабатываний: одно сообщение, которое ждёт подтверждения, и все его
повторы. Активное срабатывание у напоминания не больше одного, и это
гарантирует база, а не только код:

```sql
-- 00007_reminders.sql
CREATE UNIQUE INDEX reminder_occurrences_active_uidx
    ON reminder_occurrences (reminder_id)
    WHERE status = 'active';
```

| Колонка | Тип | Смысл |
|---|---|---|
| `id` | `uuid` | |
| `reminder_id` | `uuid` → `reminders` | `ON DELETE CASCADE` вместе с правилом |
| `scheduled_at` | `timestamptz` | когда срабатывание было должно, UTC |
| `first_sent_at` | `timestamptz` | когда ушло первое сообщение |
| `repeat_count` | `integer` | сколько сообщений отправлено, включая первое (в чате показывается `repeat_count - 1` повтор) |
| `next_repeat_at` | `timestamptz` | когда следующий повтор, UTC |
| `status` | `text` | `active`, `completed` или `cancelled` |
| `completed_at` | `timestamptz` | когда пользователь подтвердил |
| `deliveries` | `jsonb` | ссылки на отправленные сообщения (по умолчанию `[]`) |
| `created_at`, `updated_at` | `timestamptz` | |

Кроме частичного уникального индекса есть `reminder_occurrences_repeat_idx`
(`next_repeat_at`, только `active`) для поиска должных повторов и
`reminder_occurrences_reminder_idx` (`reminder_id, scheduled_at DESC`) для
истории.

Состояния: `active` → `completed` (подтверждение) или `active` → `cancelled`
(правка либо выключение правила). Подтверждённое и отменённое срабатывание
остаются в таблице как история; удаление напоминания удаляет их каскадом.

Формат `deliveries` — массив объектов, по одному на доставленное сообщение:

```json
[
  {
    "platform": "telegram",
    "remote_user_id": "42",
    "conversation_id": "42",
    "message_id": "1017"
  }
]
```

Ссылок ровно столько, сколько сообщений сейчас ждут подтверждения: повтор
перезаписывает массив новым сообщением (предыдущее удаляется), поэтому
подтверждение редактирует актуальное сообщение, а не устаревшее. Кнопка
«✅ Выполнено» несёт `<reminder_id>:<occurrence_id>` в `interactions.payload`,
поэтому идентификатор срабатывания генерируется до отправки сообщения, а строка
вставляется сразу после успешной доставки.

`reminders.lock_until` — аренда планировщика: тик ставит `now + 55 секунд` в
короткой транзакции и снимает её во второй транзакции, которая пишет результат.
Запись проверяет `lock_until` и `updated_at` из момента аренды, поэтому
опоздавший рабочий не перезаписывает состояние и не двигает `next_due_at`,
которое уже пересчитал более свежий тик или правка пользователя.

### `legacy_reminders_import`

Комната ожидания напоминаний старого n8n-бота «Cubly Reminder». Старая система
хранила напоминание за Telegram user id, новая — за SSO-пользователем, поэтому
импорт хранит правило вместе с аккаунтом, которому оно принадлежало, и
превращает строку в настоящее напоминание в момент привязки Telegram
(`docs/MIGRATION_FROM_N8N.md`).

| Колонка | Тип | Смысл |
|---|---|---|
| `id` | `uuid` | |
| `source_reminder_id` | `text` | `reminder_id` старой строки; `UNIQUE` — повторный импорт того же экспорта ничего не дублирует |
| `telegram_user_id` | `text` | `user_id` старого бота; по нему строку забирает привязка |
| `message` | `text` | текст напоминания |
| `timezone` | `text` | IANA-имя |
| `schedule_kind` | `text` | `weekly` или `monthly` |
| `schedule_days` | `smallint[]` | 1..7 или 1..31 |
| `time_hhmm` | `text` | `HH:MM` |
| `repeat_minutes` | `integer` | 1..1440 |
| `enabled` | `boolean` | выключенное правило остаётся выключенным и после импорта |
| `claimed_by` | `uuid` → `users` | кому уже присвоено; `NULL` — ждёт привязки |
| `claimed_at` | `timestamptz` | когда присвоено |
| `created_at` | `timestamptz` | |

Хранится только правило. Состояние в полёте старого воркфлоу
(`awaiting_confirmation`, `occurrence_id`, `current_message_id`, `lock_token`,
`lock_until`, счётчики) не переносится: присвоенное напоминание создаётся через
сервис `reminders` и начинает жизнь заново — `next_due_at` считается от текущего
момента, активных срабатываний нет.

Присвоение (`ClaimLegacyReminders`) — одна транзакция на аккаунт:

```sql
-- 00009_legacy_reminders_import.sql + queries/legacy_reminders_import.sql
WITH pending AS (
    SELECT id FROM legacy_reminders_import
    WHERE telegram_user_id = $2 AND claimed_by IS NULL
    ORDER BY created_at, id
    FOR UPDATE SKIP LOCKED
)
UPDATE legacy_reminders_import l
SET claimed_by = $1, claimed_at = now()
FROM pending
WHERE l.id = pending.id
RETURNING l.*;
```

`FOR UPDATE SKIP LOCKED` вместе с `claimed_by IS NULL` не даёт двум
одновременным привязкам (или привязке и повторному импорту) создать одно и то
же напоминание дважды: второй претендент не получает ни одной строки. Создание
напоминаний идёт в той же транзакции, поэтому «присвоено, но не создано» не
бывает. Само присвоение выполняется после коммита привязки: сбой импорта не
должен отменять вход, строки просто остаются ждать.

`claimed_by` объявлен как `ON DELETE SET NULL`: удаление пользователя не
уничтожает историю импорта, а строки снова становятся доступны для привязки.

Индекс `legacy_reminders_import_telegram_idx` (`telegram_user_id`) обслуживает
поиск строк одного аккаунта.

## Связи

```text
users (PrincipalID)
  ├── 1─* messenger_bindings      user_id, ON DELETE CASCADE
  ├── 1─* sessions                user_id, ON DELETE CASCADE
  ├── 1─* api_tokens              user_id, ON DELETE CASCADE
  ├── 1─* auth_pending            user_id, ON DELETE CASCADE, NULL пока вход не дошёл до OIDC
  ├── 1─* interactions            principal_id, ON DELETE CASCADE, NULL — действие без ограничения
  ├── 1─* reminders               user_id, ON DELETE CASCADE
  └── 1─* legacy_reminders_import claimed_by, ON DELETE SET NULL (строка снова ждёт привязки)

reminders
  └── 1─* reminder_occurrences    reminder_id, ON DELETE CASCADE
                                  активное срабатывание — не больше одного (частичный UNIQUE)
```

`messenger_bindings (platform, remote_user_id)` и
`legacy_reminders_import.source_reminder_id` — естественные уникальные ключи:
аккаунт мессенджера принадлежит одному пользователю, а повторный импорт того же
экспорта не создаёт дублей. `api_tokens.token_hash`, `sessions.token_hash` и
`interactions.token_hash` уникальны, потому что это sha256 секрета.

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
  `internal/interaction/postgres`, `internal/features/reminders`) поднимают
  `postgres:17` через testcontainers. Нужен Docker; без него тесты пропускаются
  с `t.Skip`.
- `go test ./...` — полный прогон, `make check` — то же плюс линтеры и
  проверки генерации.
