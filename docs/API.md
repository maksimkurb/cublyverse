# API и вход

HTTP-слой описан в `api/openapi.yaml` (OpenAPI 3.0.3). Спецификация — source of
truth: из неё генерируются Go-сервер и TypeScript-клиент.

## Конвейер

```text
api/openapi.yaml ──┬─ oapi-codegen ───────→ internal/httpapi/gen (strict server + типы)
                   └─ openapi-typescript ─→ web/src/api/schema.d.ts
зарегистрированные MCP-инструменты ───────→ docs/MCP.md (блок между маркерами)
```

- `make gen` запускает оба генератора, `sqlc` и переписывает сгенерированный
  блок `docs/MCP.md`.
- `make check` через `scripts/check-gen.sh` перегенерирует всё и падает, если
  содержимое изменилось: забыть `make gen` нельзя.
- `make check` также запускает `cublyverse docs api --check`: каждая операция
  обязана иметь `operationId`, `summary`, `description` и объявленный тег, а
  каждая схема и её поле — `description`. Проверка работает офлайн, без
  зависимостей и без базы.
- Живая документация: `/api/docs` (Swagger UI), сама спецификация —
  `/api/openapi.yaml`. Swagger UI грузится с CDN, поэтому странице нужен
  интернет; сама спецификация доступна всегда.
- Сгенерированный код (`internal/httpapi/gen`, `web/src/api/schema.d.ts`,
  `internal/store/db`) не правится руками.

### Как добавить операцию

1. Добавить путь в `api/openapi.yaml`: `operationId`, `summary`,
   `description`, тег, `security`, ответы со схемами, примеры у полей.
2. `make gen` — обновляются strict-интерфейс, типы и TS-клиент.
3. Реализовать метод в `internal/httpapi/<feature>.go` (тонкий адаптер:
   разобрать запрос, вызвать сервис, отобразить ошибку). Не забыть добавить
   зависимость в `httpapi.Deps` (`internal/httpapi/server.go`) и передать её в
   `internal/app/app.go`.
4. Для PAT осознанно решить, доступна ли операция токену: маппинг
   `patPermission` в `internal/httpapi/session.go` по умолчанию оставляет новую
   операцию только для сессии.
5. Добавить тест в `internal/httpapi` или в интеграционные тесты сервиса.
6. Обновить `docs/API.md` и, если появились переменные окружения,
   `.env.example` и `docs/DEPLOY.md`. Полный рецепт фичи, включая миграцию,
   сервис, бота и MCP, — в [`ADDING_A_FEATURE.md`](ADDING_A_FEATURE.md).

### Группы операций

| Тег | Операции | Назначение |
|---|---|---|
| `system` | `GET /health` | версия и живость HTTP-процесса; без аутентификации. |
| `identity` | `GET /me`, `PATCH /me` | профиль, привязки и часовой пояс текущего пользователя. |
| `auth` | `POST /auth/logout`, `POST /auth/telegram` | сессия браузера/Mini App; HTML-страницы входа живут вне спецификации. |
| `reminders` | `GET/POST /reminders`, `GET/PUT/DELETE /reminders/{id}`, `POST /reminders/{id}/complete` | правила напоминаний и подтверждение активного срабатывания. |
| `tokens` | `GET/POST /tokens`, `DELETE /tokens/{id}` | персональные токены для MCP и скриптов; только сессия. |
| `connections` | `GET /connections`, `DELETE /connections/{id}` | приложения, подключённые через OAuth (MCP-клиенты); только сессия. Протокольные адреса `/oauth/*` и `/.well-known/*` в OpenAPI не входят: они описаны в `docs/MCP.md`. |
| `workspaces` | `GET/POST /workspaces`, `PATCH/DELETE /workspaces/{id}`, `GET/POST /workspaces/{id}/members`, `DELETE /workspaces/{id}/members/{user_id}`, `GET /workspaces/{id}/member-candidates` | общие пространства и их участники; только сессия. |
| `craft` | `GET /craft/categories`, `GET /craft/manufacturers`, `GET/POST /workspaces/{id}/catalog`, `GET/PUT/DELETE /workspaces/{id}/catalog/{item_id}`, `POST /workspaces/{id}/catalog/{item_id}/link`, `GET /workspaces/{id}/inventory`, `PUT/PATCH/DELETE /workspaces/{id}/inventory/{item_id}`, `POST /workspaces/{id}/inventory/{item_id}/adjust` | каталог материалов и запасы пространства; только сессия. |

Все операции, кроме `GET /health` и `POST /auth/telegram`, требуют
`bearerAuth` или `cookieAuth`; `POST /auth/logout` тоже работает без сессии,
потому что идемпотентен. `/auth/*`-страницы (OIDC, подтверждение привязки)
описаны ниже, но не входят в `api/openapi.yaml`, потому что отдают HTML, а не
JSON.

Пробы инфраструктуры не входят в спецификацию: `GET /healthz` (процесс жив,
текстовый `ok`) и `GET /readyz` (`200 {"status":"ok"}` или `503` при
недоступной базе). Их использует Docker healthcheck и оркестратор, а не клиент
API.

### Базовый URL и ошибки

Все операции — под `/api/v1`. Ошибка всегда JSON:

```json
{"code": "not_found", "message": "Страница не найдена"}
```

`code` стабилен и является частью контракта, `message` — человеческий текст на
русском и может меняться.

| `code` | Когда |
|---|---|
| `bad_request` | тело или параметры не разбираются, неизвестный часовой пояс |
| `unauthorized` | нет сессии или она недействительна; неизвестный, просроченный или отозванный PAT |
| `forbidden` | сессия есть, но действие запрещено (CSRF-правило, недостаточный scope токена, PAT на сессионной операции) |
| `conflict` | состояние не позволяет действие (например, нет активного срабатывания) |
| `invalid_init_data` | подпись, формат или срок `initData` Telegram |
| `unavailable` | функция не сконфигурирована (например, нет `TELEGRAM_BOT_TOKEN`) |
| `not_found` | неизвестный путь |
| `internal` | паника или непредвиденная ошибка |

Страницы входа используют тот же формат JSON для `POST /auth/telegram/verify` и
добавляют два кода:

| `code` | Когда |
|---|---|
| `identity_mismatch` | `initData` принадлежит не тому Telegram-аккаунту, для которого создана ссылка входа (`403`). |
| `link_expired` | ссылка входа/подтверждения истекла или уже использована (`410`). |

## Модель аутентификации

Один вид сессии — непрозрачный токен из 32 случайных байт. В PostgreSQL
хранится только sha256; отзыв мгновенный. Два транспорта:

| Транспорт | Кто использует | Заголовок | Срок |
|---|---|---|---|
| `bearerAuth` | Telegram Mini App | `Authorization: Bearer <token>` | 24 часа |
| `cookieAuth` | браузер | cookie `cv_session` | 30 дней |

Правила:

- Bearer-сессия принимается только из заголовка, cookie-сессия — только из
  cookie; перепутать их нельзя.
- Cookie: `HttpOnly`, `SameSite=Lax`, `Secure`, если `PUBLIC_BASE_URL`
  начинается с `https://`, `Path=/`.
- Изменяющие запросы с cookie-сессией обязаны иметь
  `Content-Type: application/json` (иначе `403`). Это вместе с `SameSite=Lax`
  закрывает CSRF: HTML-форма не может отправить такой тип.
- `POST /api/v1/auth/logout` отзывает сессию и возвращает `Set-Cookie` с
  истёкшей cookie. Операция идемпотентна: без сессии тоже `204`.

Экраны, которые не являются JSON API, живут под `/auth/*` и описаны ниже.

## Персональные токены (PAT)

Внешние клиенты — MCP-сервер, скрипты, CI — не имеют cookie-сессии, поэтому
ходят с персональным токеном:

```http
Authorization: Bearer cv_pat_…
```

| Свойство | Значение |
|---|---|
| формат | `cv_pat_` + 43 символа URL-safe base64 (32 случайных байта) |
| хранение | в БД только sha256 и видимый префикс (8 символов после маркера) |
| выдача | `POST /api/v1/tokens`, plaintext возвращается **один раз** |
| срок | 1..365 дней или бессрочно (`expires_in_days: null`) |
| отзыв | `DELETE /api/v1/tokens/{id}`, действует мгновенно на всех репликах |
| `last_used_at` | обновляется не чаще раза в минуту на токен |

Области доступа (scopes) проверяются middleware по операции:

| Scope | Операции |
|---|---|
| `profile:read` | `GET /me` (выдаётся неявно при любом другом scope) |
| `reminders:read` | `GET /reminders`, `GET /reminders/{id}` |
| `reminders:write` | `POST/PUT/DELETE /reminders`, `POST /reminders/{id}/complete` |

Операции тегов `workspaces` и `craft` доступны только интерактивной сессии:
`patPermission` не маппит их на области токена, поэтому токен получает `403`.
MCP-доступ к «Бусинкам» идёт через отдельный endpoint `/mcp/w/{id}` с
областями `craft:read`/`craft:write` (`docs/MCP.md`).

Правила:

- неизвестный, просроченный и отозванный токен неотличимы: все дают `401`;
- недостаточная область — `403 forbidden` с общим `Error`; какой scope нужен,
  написано в документации операции, а не в тексте ошибки;
- токен **не может** управлять токенами (`GET/POST /tokens`,
  `DELETE /tokens/{id}`) и менять профиль (`PATCH /me`): эти операции
  сессионные и отвечают токену `403`. Утёкший токен не может выдать себе новый
  или расширить права;
- неудачные попытки аутентификации по PAT ограничены в памяти процесса: 20
  попыток на remote IP с восстановлением по одной за 2 секунды; успешный запрос
  очищает корзину. Это базовая защита базы от перебора, полноценный rate
  limiting — на reverse proxy;
- cookie-сессия и PAT не взаимозаменяемы: cookie принимается только из cookie,
  PAT — только из `Authorization`.

Операции тега `tokens`:

| Метод и путь | Назначение |
|---|---|
| `GET /tokens` | все токены пользователя, включая отозванные и истёкшие |
| `POST /tokens` | создать токен (`201`, plaintext в ответе один раз) |
| `DELETE /tokens/{id}` | отозвать; повторный отзыв — no-op, чужой id — `404` |

Секрет не попадает ни в список, ни в логи. Потерянный токен не
восстанавливается: его отзывают и создают новый.

## Вход из Telegram Mini App

```text
Mini App ──POST /api/v1/auth/telegram {init_data}──────────────→ сервер
   │                                                              │
   │   подпись HMAC(bot token), auth_date ≤ 24 ч, user.id          │
   │                                                              │
   ├─ привязка есть → {status:"ok", token, expires_at, user}       │
   └─ привязки нет → {status:"link_required", link_url}            │
```

- `init_data` — строка `Telegram.WebApp.initData`. Проверяются HMAC с токеном
  бота и свежесть `auth_date` (24 часа). Неверная подпись, чужой бот и
  просроченная дата неотличимы: `401 invalid_init_data`.
- Telegram user id берётся только из подписанных данных. Поэтому `link_url`
  создаётся для реального владельца аккаунта, а не для того, кого прислал
  клиент.
- `link_url` — адрес страницы входа. Внутри Mini App по нему нужно переходить в
  том же webview (`window.location.assign`), тогда `initData` остаётся
  доступным на следующей странице.

## Привязка мессенджера (страницы `/auth/*`)

Привязка — двухшаговый процесс. Без второго шага SSO-аккаунт жертвы мог бы
привязаться к мессенджеру атакующего: ссылку входа легко переслать.

```text
бот: «Войти» (OpenApp)
   │
   ▼
GET /auth/login?state=…            ← state из PG, TTL 10 минут
   │  Telegram: страница грузит telegram-web-app.js и отправляет initData
   ▼
POST /auth/telegram/verify {state, init_data}
   │  подпись + совпадение user.id с ожидающей привязкой
   │  иначе 403 identity_mismatch, дальше не пускаем
   ▼
302 на OIDC-провайдера (PKCE S256 + nonce)
   │
   ▼
GET /auth/callback?code=…&state=…
   │  обмен кода, проверка nonce, UpsertFromOIDC → users
   │  Telegram: без init_data_verified_at привязка не продолжается
   ▼
страница подтверждения (state + одноразовый confirm-токен)
   │
   ▼
POST /auth/confirm (form: state, token)
   │  сравнение sha256(token) в постоянном времени, атомарный переход
   │  состояния, затем привязка в одной транзакции
   ▼
«✅ Аккаунт привязан» + сообщение в чат
```

Свойства:

- `state`, confirm-токен, nonce и PKCE verifier — по 32 случайных байта.
  State и confirm-токен хранятся как sha256; сравнение confirm-токена —
  `crypto/subtle`.
- Одноразовость обеспечивает PostgreSQL: переходы статусов
  (`pending_oidc` → `awaiting_confirmation` → `completed`) выполняются
  условным `UPDATE … WHERE status = … RETURNING`. Повторный callback, повторный
  POST подтверждения и повторный `verify` получают `410`.
- Страница подтверждения показывает платформу, id мессенджера и имя
  SSO-аккаунта; до нажатия «Привязать» строки в `messenger_bindings` нет.
- После подтверждения бот отправляет в чат «✅ Аккаунт привязан» и кнопку
  «Открыть приложение» (`OnLinked` в `internal/app`).

### Страницы

| Метод и путь | Назначение |
|---|---|
| `GET /auth/login` | Старт входа в браузере; `return_to` — относительный путь |
| `GET /auth/login?state=…` | Продолжение входа из бота или Mini App |
| `GET /auth/callback` | Возврат от OIDC-провайдера |
| `POST /auth/telegram/verify` | Проверка `initData` для ожидающей Telegram-привязки |
| `POST /auth/confirm` | Подтверждение привязки (form: `state`, `token`) |

`GET /auth/login`, `GET /auth/callback` и результат `POST /auth/confirm`
отдают HTML на русском, не кэшируются (`Cache-Control: no-store`) и запрещают
отправку `Referer` (`Referrer-Policy: no-referrer`), чтобы state не утекал
провайдеру. `POST /auth/telegram/verify` — исключение: это JSON-эндпоинт,
который вызывает страница входа из Mini App.

### `return_to`

Принимается только относительный путь этого сайта: `/app/`,
`/app/?tab=reminders`. Отклоняются абсолютные URL, `//host`, `/\host`,
`scheme:`, управляющие символы и значения длиннее 2048 байт. Иначе `400`:
искать открытый редирект не придётся.

### Вход из браузера

`GET /auth/login?return_to=/app/` → OIDC → callback ставит cookie-сессию и
отвечает `303` на `return_to` (по умолчанию `/app/`). Привязки мессенджера при
этом не создаются.

## Сессии в WebApp

- Mini App держит токен в памяти и отправляет `Authorization: Bearer …`.
  Каждое открытие приложения присылает свежий `initData` и получает новую
  сессию; старые отзываются по сроку.
- Браузер ничего не знает о токене: cookie `HttpOnly` ставится на callback.
- `GET /api/v1/me` возвращает профиль и привязки, `PATCH /api/v1/me` меняет
  часовой пояс (IANA-имя, проверяется через `time.LoadLocation`).
- Как WebApp хранит токен, повторяет запрос после `401` и показывает экраны
  входа — в [WEBAPP.md](WEBAPP.md).

## Напоминания

Операции тега `reminders`. Семантика расписания, состояние срабатывания и
правило перехода на летнее время — в [FEATURES.md](FEATURES.md).

| Метод и путь | Назначение |
|---|---|
| `GET /reminders` | список напоминаний пользователя с активными срабатываниями |
| `POST /reminders` | создать напоминание (`201`), `repeat_minutes` по умолчанию 10, `enabled` — `true` |
| `GET /reminders/{id}` | одно напоминание |
| `PUT /reminders/{id}` | полная замена правила; пропущенное поле не сохраняется |
| `DELETE /reminders/{id}` | удалить напоминание вместе с историей (`204`) |
| `POST /reminders/{id}/complete` | подтвердить активное срабатывание |

Все операции требуют сессию (`bearerAuth` или `cookieAuth`) и работают только с
напоминаниями её владельца. Чужой идентификатор и несуществующий неотличимы:
оба дают `404 not_found`.

Правила, которые проверяет сервис (`ValidationError` → `400 bad_request` с
русским текстом в `message`):

- сообщение 1..1500 символов (руны, пробелы по краям обрезаются);
- `repeat_minutes` 1..1440;
- хотя бы один день; `weekly` — 1..7 (Пн=1), `monthly` — 1..31;
- время строго `HH:MM`, 24 часа, с ведущим нулём;
- часовой пояс — существующее IANA-имя (`Local` запрещён).

Ответы и семантика:

- `next_due_at` — ближайшее срабатывание в UTC, `null` у выключенного
  напоминания. При создании и правке оно считается от текущего времени: если
  время сегодня уже прошло, срабатывание будет в следующий подходящий день;
- `PUT` отменяет активное срабатывание (старое сообщение больше не актуально) и
  пересчитывает `next_due_at`; `enabled: false` очищает `next_due_at`;
- `POST .../complete` подтверждает активное срабатывание и отвечает `200` с
  напоминанием без `active_occurrence`. Если подтверждать нечего (ещё не
  срабатывало, уже подтверждено или отменено правкой) — `409 conflict`;
- у напоминания не больше одного активного срабатывания, это гарантирует
  частичный уникальный индекс в БД;
- при переводе часов вперёд несуществующее локальное время сдвигается вперёд на
  длину «дырки», при переводе назад повторяющееся время срабатывает один раз, в
  первое совпадение;
- cookie-сессия обязана присылать `Content-Type: application/json` у всех
  изменяющих запросов, включая `DELETE` и `complete` без тела, иначе `403
  forbidden`.


## Пространства

Пространство (workspace) — именованное место, все участники которого имеют
одинаковые права на его данные. Пользователь может состоять в нескольких
пространствах. Приглашений по ссылке нет: участника добавляют, выбирая
пользователя, который хотя бы раз входил в Cublyverse.

| Метод и путь | Назначение |
|---|---|
| `GET /workspaces` | пространства пользователя, по имени |
| `POST /workspaces` | создать (`201`); создатель становится первым участником |
| `PATCH /workspaces/{id}` | переименовать (`name`, 1..80 символов) |
| `DELETE /workspaces/{id}` | удалить пространство со всеми данными (`204`) |
| `GET /workspaces/{id}/members` | участники с `avatar_url` (Gravatar) |
| `POST /workspaces/{id}/members` | добавить участника (`user_id`); повторное добавление — успех |
| `DELETE /workspaces/{id}/members/{user_id}` | убрать участника; себя — «выйти» |
| `GET /workspaces/{id}/member-candidates?query=` | кто ещё может войти; фильтр по имени или e-mail |

Правила:

- чужое пространство неотличимо от несуществующего: `404 not_found`;
- последнего участника удалить нельзя — `409 conflict`, удаляйте пространство;
- у участников и кандидатов есть `avatar_url` — Gravatar от e-mail с
  identicon-фолбэком;
- добавление уже состоящего участника идемпотентно.

## Каталог и запасы

Операции тега `craft`. Справочники (`GET /craft/categories`,
`GET /craft/manufacturers`) общие для всех пространств. Позиция каталога либо
общая (её пишут скраперы, менять нельзя), либо частная для пространства.

| Метод и путь | Назначение |
|---|---|
| `GET /craft/categories` | категории: `slug`, имя, единица по умолчанию |
| `GET /craft/manufacturers` | известные производители |
| `GET /workspaces/{id}/catalog?category=&manufacturer=&q=&only_in_stock=&limit=&offset=` | поиск по общему каталогу и частным позициям; в ответе остаток пространства |
| `POST /workspaces/{id}/catalog` | создать частную позицию (`201`) |
| `GET/PUT/DELETE /workspaces/{id}/catalog/{item_id}` | одна позиция; менять и удалять можно только частную |
| `POST /workspaces/{id}/catalog/{item_id}/link` | связать частную позицию с каталожной: остаток переезжает, частная запись удаляется |
| `GET /workspaces/{id}/inventory?category=&manufacturer=&q=&max_qty=&limit=&offset=` | запасы пространства |
| `PUT /workspaces/{id}/inventory/{item_id}` | поставить точное количество (пересчёт) |
| `POST /workspaces/{id}/inventory/{item_id}/adjust` | изменить на `delta`: плюс — добавить, минус — списать |
| `PATCH /workspaces/{id}/inventory/{item_id}` | заменить `location` и `notes` строки запаса |
| `DELETE /workspaces/{id}/inventory/{item_id}` | убрать со склада; позиция каталога остаётся |

Ответы об ошибках:

| Ошибка сервиса | HTTP |
|---|---|
| `workspace.ErrNotFound`, `craft.ErrNotFound` | `404 not_found` |
| `workspace.ValidationError`, `craft.ValidationError` | `400 bad_request`, текст на русском в `message` |
| `craft.ErrReadOnly` | `403 forbidden` |
| `craft.ErrInsufficientStock` | `409 conflict` |

Списание больше остатка — `409 conflict`; строка запаса создаётся при первом
изменении, поэтому «добавить на склад» — это `adjust` с положительной дельтой.
Единицы частной и целевой позиции при связывании должны совпадать, иначе
`400 bad_request`.

## TypeScript-клиент

`web/src/api/client.ts` использует `openapi-fetch` с типами из
`web/src/api/schema.d.ts`. Адреса относительные (`/api/v1`), потому что и
WebApp, и API отдаёт один бинарник; в dev-режиме Vite проксирует `/api` и
`/auth` на бэкенд.

## Ограничения

- Rate limiting на `/auth/*` и остальной API — в бэклоге (`docs/PLAN.md` §7);
  для PAT работает базовая корзина неудачных попыток (см. выше).
- Вход Element-виджета через Matrix OpenID — в бэклоге.
- OAuth 2.1 для MCP — в бэклоге; на M5 используется PAT (`docs/MCP.md`).
