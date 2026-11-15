# Развёртывание

Приложение — один бинарник, которому нужны только PostgreSQL и переменные
окружения. WebApp встроен в бинарник (`embed`), миграции тоже.

## Переменные окружения

Полный список; значения по умолчанию указаны явно. Секреты (токены, пароли,
client secret) живут только в окружении и никогда не попадают в репозиторий.

| Переменная | Обязательна | По умолчанию | Назначение |
|---|---|---|---|
| `DATABASE_URL` | да | — | `postgres://…` или `postgresql://…` |
| `HTTP_ADDR` | нет | `:8080` | адрес HTTP-сервера |
| `PUBLIC_BASE_URL` | для бота | — | внешний адрес без слэша на конце; из него строятся ссылки в мессенджерах, `OIDC_REDIRECT_URL` и флаг `Secure` у cookie |
| `LOG_FORMAT` | нет | `text` | `text` или `json` |
| `OIDC_ISSUER` | для входа | — | issuer провайдера; при старте выполняется discovery |
| `OIDC_CLIENT_ID` | при `OIDC_ISSUER` | — | id клиента |
| `OIDC_CLIENT_SECRET` | нет | — | секрет клиента (если провайдер требует) |
| `OIDC_REDIRECT_URL` | нет | `PUBLIC_BASE_URL + /auth/callback` | абсолютный callback, зарегистрированный у провайдера |
| `TELEGRAM_BOT_TOKEN` | для Telegram | — | токен бота; им же проверяется подпись `initData` Mini App |
| `MATRIX_HOMESERVER` | для Matrix | — | URL homeserver |
| `MATRIX_USERNAME` | при `MATRIX_HOMESERVER` | — | логин бота |
| `MATRIX_PASSWORD` | при `MATRIX_HOMESERVER` | — | пароль бота |
| `MATRIX_CRYPTO_DB` | нет | `matrix-crypto.db` | файл SQLite с криптосостоянием; должен переживать перезапуск |
| `MATRIX_PICKLE_KEY` | при `MATRIX_HOMESERVER` | — | ключ шифрования криптосостояния, не короче 16 байт |

Правила валидации (`internal/config`):

- без `DATABASE_URL` процесс не стартует и перечисляет недостающие переменные;
- `OIDC_ISSUER` без `OIDC_CLIENT_ID` — ошибка; без `OIDC_REDIRECT_URL` и
  `PUBLIC_BASE_URL` тоже (callback некуда вести);
- `MATRIX_HOMESERVER` без логина, пароля или pickle-ключа — ошибка;
- `PUBLIC_BASE_URL` и `OIDC_REDIRECT_URL` обязаны быть абсолютными http(s) URL.

Если транспорт настроен, но `OIDC_ISSUER` нет, транспорт не запускается: без
входа пользователь не сможет привязать аккаунт. В лог пишется предупреждение.

## Быстрый старт (Docker Compose)

```bash
docker compose -f deploy/docker-compose.yml up --build
```

Поднимаются `postgres:17` и приложение, готовность проверяется через `/readyz`.
Compose-файл содержит только dev-значения.

## Развёртывание вручную

```bash
make build                                  # web/dist + bin/cublyverse
DATABASE_URL=… PUBLIC_BASE_URL=https://… OIDC_ISSUER=… OIDC_CLIENT_ID=… \
TELEGRAM_BOT_TOKEN=… ./bin/cublyverse serve
```

`serve` сам применяет миграции и поднимает HTTP-сервер. Остановка — по
SIGINT/SIGTERM, на завершение в работе даётся 10 секунд.

Проверки живости:

- `GET /healthz` — процесс жив (не трогает базу);
- `GET /readyz` — база отвечает (503, если нет).

## Требования к окружению

- **HTTPS.** Telegram принимает `web_app`-кнопки только на https-адресах, а
  cookie-сессии получают `Secure` именно при `https://` в `PUBLIC_BASE_URL`.
  Для локальной разработки допустим http, но бот в Telegram с http-кнопкой
  работать не будет.
- **tzdata.** Проверка часовых поясов использует `time.LoadLocation`, то есть
  системную базу зон. В образе `deploy/Dockerfile` пакет `tzdata` установлен;
  в минимальном контейнере без него принимается только `UTC`.
- **Постоянный диск для Matrix.** `MATRIX_CRYPTO_DB` (SQLite) нельзя терять:
  иначе устройство Matrix перестанет расшифровывать историю. В контейнере
  кладите файл на том (рабочий каталог `/home/cublyverse`).
- **Один токен бота — один поллер.** Нельзя одновременно держать запущенными
  n8n-воркфлоу и этот сервис с тем же `TELEGRAM_BOT_TOKEN`.

## OIDC-клиент

В провайдере зарегистрируйте redirect URI:

```text
PUBLIC_BASE_URL + /auth/callback   (или OIDC_REDIRECT_URL)
```

Требуется поддержка Authorization Code + PKCE (S256) и ID token с `nonce`.
Scopes: `openid profile email`. Email и имя необязательны: без них профиль
покажет только id, а страница подтверждения — «аккаунту SSO».

Discovery провайдера выполняется при старте сервиса: если провайдер недоступен,
процесс не поднимется с понятной ошибкой. Это осознанный выбор — молча
запуститься с нерабочим входом хуже, чем упасть на старте.

## Обновление

1. Собрать новый образ или бинарник.
2. Применить миграции: `cublyverse migrate status` (показывает неприменённые),
   затем перезапуск — `serve` применит их сам.
3. Проверить `/readyz` и вход в WebApp.

Миграции только добавляющие (см. `docs/DATABASE.md`), поэтому откат кода на
предыдущую версию безопасен, пока не удалены устаревшие колонки.

## Бэкапы

```bash
pg_dump --format=custom --no-owner --dbname "$DATABASE_URL" > "cublyverse-$(date +%F).dump"
```

Восстановление — в чистую базу:

```bash
createdb cublyverse_restore
pg_restore --no-owner --dbname cublyverse_restore cublyverse.dump
```

Файл Matrix-криптосостояния бэкапится вместе с томом (для одного устройства
он небольшой). Секретов в дампе нет: пароли, токены сессий, state и
confirm-токены хранятся как sha256.

## Переключение с n8n

Полный чек-лист появится в M6-2. Порядок такой:

1. Выключить n8n-воркфлоу «Cubly Reminder» (освободить токен бота).
2. Прописать новые переменные, поднять сервис, проверить `/readyz`.
3. Выполнить `cublyverse import-n8n <file>` (M6-1) — старые напоминания ждут
   привязки по Telegram user_id.
4. Написать боту с нового аккаунта, пройти привязку, убедиться, что
   напоминания перенеслись.
5. Отправить тестовое напоминание от начала до конца.

## Диагностика

- `403` на `PATCH /me` из браузера — клиент не отправил
  `Content-Type: application/json` (CSRF-правило, см. `docs/API.md`).
- `503 unavailable` на `POST /api/v1/auth/telegram` — не задан
  `TELEGRAM_BOT_TOKEN`.
- Страница `/auth/login` пишет «Откройте эту страницу из чата с ботом» — ссылка
  открыта в браузере, а не в Mini App; `initData` без Telegram недоступен.
- `410` на странице входа — ссылка старше 10 минут или уже использована:
  начните вход заново из чата.
