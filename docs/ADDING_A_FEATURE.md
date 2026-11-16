# Как добавить фичу

Пошаговый рецепт на примере напоминаний — единственной готовой фичи. Пути к
файлам настоящие: их можно открыть и повторить структуру для своей фичи.
Общие правила — [`../AGENTS.md`](../AGENTS.md), устройство слоёв —
[`ARCHITECTURE.md`](ARCHITECTURE.md).

Ниже «фича» — самостоятельный модуль продукта. У неё один сервис с
бизнес-логикой и три тонких адаптера: бот, HTTP API и MCP. WebApp — четвёртый
вход, но он ходит через HTTP API, а не через сервис напрямую.

```text
internal/features/<feature>/     сервис, расписание, бот, MCP, планировщик
api/openapi.yaml                 HTTP-контракт (source of truth)
internal/httpapi/<feature>.go    тонкий HTTP-адаптер
internal/app/app.go              сборка: сервис → реестр → API → бот → MCP
web/src/features/<feature>/      экран WebApp + строка в реестре
```

## 0. Продумать модель и права

До кода ответьте на четыре вопроса:

1. **Владелец.** Кто владеет данными? В Cublyverse это всегда SSO-пользователь
   (`PrincipalID` = `users.id`), а не аккаунт мессенджера. Напоминание переживает
   перепривязку Telegram, потому что `reminders.user_id` ссылается на `users`.
2. **Инварианты.** Что должно быть истинным всегда? У напоминания — не больше
   одного активного срабатывания; это гарантирует частичный уникальный индекс
   `reminder_occurrences_active_uidx`, а не только код.
3. **Входы.** Какие операции нужны боту, API и MCP? Если операция нужна двум
   входам, она живёт в сервисе, а не в адаптере.
4. **Фоновые задачи.** Есть ли работа по расписанию? У напоминаний это
   `reminders.scheduler`, который тикает каждые `REMINDERS_TICK_INTERVAL`.

## 1. Миграция

Схема меняется только новой миграцией goose. Применённые миграции не правятся.

```text
internal/store/migrations/00007_reminders.sql     пример готовой миграции
internal/store/migrations/00010_<тема>.sql        новая фича
```

Правила:

- имя — `NNNNN_<тема>.sql`, номер сквозной и не переиспользуется;
- в файле есть `-- +goose Up` и `-- +goose Down`; Down нужен для локальных
  экспериментов, в продакшене откат — восстановление дампа;
- только добавляющие изменения в одном релизе (новая таблица, nullable-колонка,
  новый индекс); удаление и переименование — двумя шагами;
- `CHECK`-ограничения дублируют правила сервиса, чтобы некорректная строка не
  появилась в обход кода;
- для больших таблиц `CREATE INDEX CONCURRENTLY` выносится в миграцию с
  `-- +goose NO TRANSACTION`.

Пример структуры таблицы — `reminders` и `reminder_occurrences` в
`00007_reminders.sql`: владелец, `CHECK`-и, индексы для планировщика и списка,
частичный уникальный индекс для инварианта.

После файла:

```bash
make migrate         # применить локально (serve делает это и сам)
make check           # миграции накатываются на чистую БД в интеграционных тестах
```

Подробности — [`DATABASE.md`](DATABASE.md).

## 2. Запросы sqlc

SQL живёт в `internal/store/queries/<тема>.sql`, сгенерированный код — в
`internal/store/db` (руками не правится).

```text
internal/store/queries/reminders.sql
```

- каждая функция помечена `-- name: Имя :one|:many|:exec|:execrows`;
- выборка данных ограничена владельцем: `WHERE id = $1 AND user_id = $2`;
- чужая и несуществующая строка неотличимы (обе дают `pgx.ErrNoRows`);
- агрегаты и списки — без N+1: у напоминаний `ListReminders` +
  `ListActiveOccurrencesByUser` дают весь ответ за два запроса;
- блокировки — `FOR UPDATE SKIP LOCKED` и аренда (`lock_until`), а не
  долгая транзакция на время сетевого вызова.

```bash
make gen-go          # oapi-codegen + sqlc
```

Правило: параметры и результаты — типы sqlc (`pgtype.UUID`,
`pgtype.Timestamptz`, `[]byte`), преобразование id — хелперами
`internal/auth` (`formatUUID`, `parseUUID`) или аналогичными в пакете фичи.

## 3. Сервис фичи

Сервис — единственная точка бизнес-логики. Все методы принимают `PrincipalID`
и ограничивают каждый запрос владельцем.

```text
internal/features/reminders/service.go   сервис: Create, List, Get, Update, Delete, Complete
internal/features/reminders/feature.go   Feature: Name, RegisterBot, RegisterMCP, Jobs
internal/features/reminders/schedule/    чистые функции расписания
```

Что важно повторить:

- **Вход и проверка.** `reminders.Input` — то, что приходит от адаптеров;
  `canonicalize` — единственная копия проверок (длина, часовой пояс, время,
  дни, интервал). Ошибка — `*reminders.ValidationError` с русским текстом.
- **Часы.** `reminders.WithClock(func() time.Time)` позволяет тестам и
  планировщику работать с управляемым временем.
- **Ошибки.** `ErrNotFound` (нет такого или чужой), `ErrNoActiveOccurrence`
  (нечего подтверждать), `ErrLeaseLost` (аренда потеряна). HTTP, бот и MCP
  переводят их в свои ответы, но не придумывают новые правила.
- **Транзакции.** Несколько изменений — `store.InTx(func(q *db.Queries) error)`.
  У `Update` это строка правила + отмена активного срабатывания + новый
  `next_due_at` в одной транзакции.
- **Контракт фичи.** Реализуйте `feature.Feature`:

  ```go
  type Feature interface {
      Name() string
      RegisterBot(r *bot.Router)
      RegisterMCP(s *mcpserver.Server)
      Jobs() []Job
  }
  ```

  Пакет `internal/feature` существует, чтобы фича не импортировала
  `internal/app` (это дало бы цикл: `internal/app` сам собирает фичи).

Сервис не импортирует `unibot/platform/*`. Доставка в мессенджер — интерфейс
`reminders.Delivery` (`Send`, `Edit`, `Delete`), реализация живёт в
`internal/app/delivery.go` и ходит через `unibot.Runtime`.

## 4. OpenAPI и HTTP

`api/openapi.yaml` — источник правды. Сначала спецификация, потом код.

```text
api/openapi.yaml                     пути, схемы, security, примеры, теги
internal/httpapi/reminders.go        реализация strict-интерфейса
internal/httpapi/server.go           Deps.Reminders и монтирование
internal/app/app.go                  создание сервиса и передача в httpapi.Deps
```

Порядок:

1. Добавьте тег (если новый), пути, `operationId`, `summary`, `description`,
   `security`, ответы, схемы и примеры. Каждая операция и каждая схема/поле
   должны быть описаны: это проверяет `cublyverse docs api --check` в
   `make check`.
2. `make gen` — появляются методы strict-интерфейса и типы в
   `internal/httpapi/gen`.
3. Реализуйте методы в `internal/httpapi/<feature>.go`. Обработчик только
   разбирает запрос, вызывает сервис и переводит ошибки:
   `*ValidationError` → `400 bad_request`, `ErrNotFound` → `404 not_found`,
   `ErrNoActiveOccurrence` → `409 conflict`, остальное → `500 internal`.
4. Добавьте зависимость в `httpapi.Deps` (`internal/httpapi/server.go`) и
   передайте сервис в `internal/app/app.go`.
5. Тесты: `httptest` для формы ответа и интеграционные тесты сервиса для
   бизнес-правил. Пример — `internal/features/reminders/http_integration_test.go`.
6. Обновите [`API.md`](API.md) и таблицу эндпоинтов.

Помните про CSRF-правило: изменяющие запросы с cookie-сессией обязаны присылать
`Content-Type: application/json`; Bearer-сессия и PAT этому правилу не
подчиняются. PAT получает доступ только к операциям, перечисленным в
`patPermission` (`internal/httpapi/session.go`); новая операция по умолчанию
доступна только сессии — это безопасное поведение.

## 5. Бот

```text
internal/features/reminders/bot.go        команда /reminders и префикс действий
internal/features/reminders/messages.go   тексты, HTML, кнопка «✅ Выполнено»
internal/bot/router.go                    диспетчер команд и действий
```

- `Feature.RegisterBot(router)` регистрирует команды и префиксы действий:
  `router.Command("reminders", "список напоминаний", handler)` и
  `router.Action("reminders.", handler)`.
- Действие — `unibot.Action` с `ID`, `Label`, `Kind`, `Payload`, опциональным
  `PrincipalID` и `TTL`. Telegram ограничивает `callback_data` 64 байтами,
  поэтому идентификаторы живут в `interactions.payload` и приходят в
  `ActionEvent.Payload`.
- Ответ на нажатие — `botctx.Ack(ctx, text, alert)`. Если обработчик не вызвал
  `Ack`, runtime отправит пустой ответ; для Matrix `Ack` — no-op.
- Тексты русские; разметка — только `b`, `i`, `code`, `a href`, пользовательские
  данные проходят через `unibot.EscapeHTML`.
- Кнопка WebApp добавляется только для https `PUBLIC_BASE_URL`
  (`router.AppAction`), потому что `web_app`-кнопки требуют https.
- Тесты роутера и обработчиков — `internal/features/reminders/bot_integration_test.go`,
  `internal/bot/router_test.go`.

Команды и префиксы регистрируются один раз при старте, поэтому в горячем пути
нет блокировок. Неизвестная команда получает подсказку `/help`, неизвестное
действие — «Это действие больше неактуально».

## 6. MCP

```text
internal/features/reminders/mcp.go       инструменты фичи
internal/mcpserver/server.go             AddTool, RequireScope, Principal
internal/auth/tokens.go                  реестр scope-ов
docs/MCP.md                              сгенерированный блок + проза
```

- `Feature.RegisterMCP(server)` вызывает `mcpserver.AddTool` с `mcp.Tool`
  (`Name`, `Title`, `Description`, `Annotations`) и типизированным
  обработчиком. Схемы вход/выход выводятся из Go-структур с `jsonschema`-тегами.
- Имя инструмента стабильно: клиенты хранят его в конфигурации. Префикс имени
  фичи (`reminders_`) не обязателен, но помогает не пересекаться.
- Scope проверяется до вызова сервиса: `mcpserver.RequireScope(principal,
  auth.ScopeRemindersRead)`. Новая область добавляется в `auth.Scopes` и в
  соответствующую проверку; миграция не нужна — в базе лежат строки.
- Ошибки сервиса превращаются в результат инструмента с `isError: true` и
  понятным текстом (`mcpServiceError`), чтобы модель могла исправить аргументы.
- `make gen` переписывает блок между маркерами в `docs/MCP.md`;
  `make check` падает, если инструмент зарегистрирован, но не задокументирован.
- Добавьте пример вызова для каждого инструмента в [`MCP.md`](MCP.md)
  (вручную, вне сгенерированного блока).

## 7. WebApp

```text
web/src/features/reminders/index.ts        FeatureScreen (id, title, icon, route, component)
web/src/features/reminders/RemindersScreen.tsx
web/src/features/reminders/ReminderEditor.tsx
web/src/features/reminders/ReminderForm.tsx
web/src/features/reminders/data.ts         запросы openapi-react-query
web/src/features/reminders/format.ts       чистые помощники
web/src/features/registry.ts               одна строка на фичу
```

- `FeatureScreen` описывает экран; навигация и роутер собираются из
  реестра `features`, отдельный маршрут регистрировать не нужно.
- Данные — через `web/src/api/query.ts` (`openapi-react-query`) и типы из
  `web/src/api/schema.d.ts`; после изменения API — `make gen`.
- Компоненты не обращаются к `Telegram.WebApp` напрямую: только через
  `HostBridge` (`web/src/host`). Это позволяет тестировать их с фейковым хостом.
- Чистые помощники (форматирование, разбор ввода, маппинг в тело запроса) — в
  отдельных модулях без React; их легко покрыть Vitest.
- Тесты — `pnpm test`; компонентные — с `src/test/render.tsx`.

Рецепт экрана и мост к хосту подробно — [`WEBAPP.md`](WEBAPP.md).

## 8. Сборка в `internal/app`

`internal/app/app.go` — единственное место, где всё соединяется:

1. `store.Migrate` → `store.Open`;
2. `auth.New` — пользователи, привязки, сессии, PAT;
3. `reminders.New(...)` — сервис с логгером, доставкой и часами;
4. `reminders.NewFeature(service, logger, opts...)` → `app.NewRegistry(...)`;
5. `mcpserver.New(...)` → `registry.RegisterMCP(server)`;
6. `httpapi.New(httpapi.Deps{... Reminders: service ...})`;
7. `startBot`: `bot.NewRouter` → `registry.RegisterBot(router)` → транспорты →
   `unibot.Runtime` → `delivery.attach(runtime)` → `OnLinked`/`OnLegacyImported`;
8. `extraJobs` — `auth.cleanup` и очистка `interactions`;
9. `Run`: `runtime.Start` → `jobs.Start(registry.Jobs() + extraJobs)` →
   `ListenAndServe` → graceful shutdown.

Добавляя фичу, расширьте этот список, а не создавайте второй экземпляр
сервиса: бот, API и MCP должны получить **тот же** сервис. Тесты сборки —
`internal/app/app_test.go`, `internal/app/features_test.go`.

## 9. Тесты

Минимум для новой фичи:

| Уровень | Где | Что проверить |
|---|---|---|
| Чистые функции | `internal/features/<feature>/..._test.go` | расписание, разбор, форматирование — без базы. |
| Сервис | `internal/features/<feature>/service_integration_test.go` | CRUD, владелец, валидация, инварианты; testcontainers `postgres:17`. |
| HTTP | `internal/features/<feature>/http_integration_test.go` | коды ответов, форма JSON, 404 на чужое. |
| Бот | `internal/features/<feature>/bot_integration_test.go` | команды, действия, все ветки `Ack`. |
| MCP | `internal/mcpserver/server_integration_test.go` | схемы, scope, ошибки, изоляция владельцев. |
| Планировщик | `internal/features/<feature>/scheduler_integration_test.go` | первое срабатывание, повтор, догон, две реплики. |
| WebApp | `web/src/features/<feature>/*.test.tsx` | форма, список, ошибки, пустое состояние. |

Без Docker интеграционные тесты пропускаются (`t.Skip`) — `make check`
остаётся зелёным, но полную проверку базы нужно делать с Docker. Управляемое
время и фейковая доставка уже есть в пакете напоминаний; переиспользуйте их.

## 10. Документация

В том же изменении обновите:

- [`DATABASE.md`](DATABASE.md) — новая миграция в списке и таблицы/колонки;
  `scripts/check-docs.sh` падает, если файла миграции нет в документе;
- [`API.md`](API.md) — новая группа операций;
- [`MCP.md`](MCP.md) — сгенерированный блок и примеры новых инструментов;
- [`FEATURES.md`](FEATURES.md) — раздел фичи;
- [`DEVELOPMENT.md`](DEVELOPMENT.md) — новая переменная окружения или make-цель;
- [`DEPLOY.md`](DEPLOY.md) — переменные окружения и эксплуатация;
- [`USER_GUIDE.md`](USER_GUIDE.md) — если фича видна пользователю;
- [`docs/README.md`](README.md) — если добавили новый документ.

`make check` проверит ссылки, индекс, миграции, make-цели, MCP-инструменты,
переменные окружения и полноту OpenAPI.

## Чек-лист

- [ ] Модель данных и права продуманы; владелец — `PrincipalID`.
- [ ] Новая goose-миграция, применённая миграция не тронута.
- [ ] sqlc-запросы с фильтром по владельцу; `make gen` проходит.
- [ ] Сервис с проверками, часами и типизированными ошибками.
- [ ] `internal/feature.Feature` реализован: `Name`, `RegisterBot`,
      `RegisterMCP`, `Jobs`.
- [ ] OpenAPI: `operationId`, `summary`, `description`, тег, security,
      примеры; `make gen`; `docs api --check` зелёный.
- [ ] HTTP-адаптер тонкий; ошибки отображаются в `Error`.
- [ ] `httpapi.Deps` и `internal/app/app.go` обновлены; сервис один.
- [ ] Команды/действия бота зарегистрированы; `Ack` отвечает; тексты русские.
- [ ] MCP-инструменты с описаниями, scope и примерами; MCP.md обновлён.
- [ ] `patPermission` в `internal/httpapi/session.go` осознанно обновлён (или
      операция оставлена только для сессии).
- [ ] Экран WebApp добавлен в `web/src/features/registry.ts`; типы из
      `schema.d.ts`.
- [ ] Юнит- и интеграционные тесты; тесты бота и MCP; тесты WebApp.
- [ ] Документация обновлена; `make check` зелёный.
