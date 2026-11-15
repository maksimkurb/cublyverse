# Развёртывание

Приложение — один бинарник, которому нужны только PostgreSQL и переменные
окружения. WebApp встроен в бинарник (`embed`), миграции тоже: `serve` сам
приводит схему к текущей версии. Внешний TLS завершает обратный прокси, база
наружу не публикуется.

Документ описывает боевую установку. Локальная разработка — `make dev` (см.
`README.md`).

## Переменные окружения

Полный список; значения по умолчанию указаны явно. Секреты (токены, пароли,
client secret) живут только в окружении и никогда не попадают в репозиторий.

| Переменная | Обязательна | По умолчанию | Назначение |
|---|---|---|---|
| `DATABASE_URL` | да | — | `postgres://…` или `postgresql://…` |
| `HTTP_ADDR` | нет | `:8080` | адрес HTTP-сервера |
| `PUBLIC_BASE_URL` | для бота | — | внешний адрес без слэша на конце; из него строятся ссылки в мессенджерах, `OIDC_REDIRECT_URL` и флаг `Secure` у cookie |
| `LOG_FORMAT` | нет | `text` | `text` или `json` (в проде — `json`) |
| `OIDC_ISSUER` | для входа | — | issuer провайдера; при старте выполняется discovery |
| `OIDC_CLIENT_ID` | при `OIDC_ISSUER` | — | id клиента |
| `OIDC_CLIENT_SECRET` | нет | — | секрет клиента (если провайдер требует) |
| `OIDC_REDIRECT_URL` | нет | `PUBLIC_BASE_URL + /auth/callback` | абсолютный callback, зарегистрированный у провайдера |
| `TELEGRAM_BOT_TOKEN` | для Telegram | — | токен бота; им же проверяется подпись `initData` Mini App |
| `MATRIX_HOMESERVER` | для Matrix | — | URL homeserver |
| `MATRIX_USERNAME` | при `MATRIX_HOMESERVER` | — | логин бота |
| `MATRIX_PASSWORD` | при `MATRIX_HOMESERVER` | — | пароль бота |
| `MATRIX_CRYPTO_DB` | нет | `matrix-crypto.db` | файл SQLite с криптосостоянием; должен переживать перезапуск (в compose — `/data/matrix-crypto.db` на томе `appdata`) |
| `MATRIX_PICKLE_KEY` | при `MATRIX_HOMESERVER` | — | ключ шифрования криптосостояния, не короче 16 байт |
| `BOT_HANDLER_TIMEOUT` | нет | `30s` | предел выполнения одного обработчика бота; формат Go-длительности (`30s`, `2m`) |
| `REMINDERS_TICK_INTERVAL` | нет | `15s` | как часто планировщик напоминаний ищет должные срабатывания; формат Go-длительности (`15s`, `1m`) |

Правила валидации (`internal/config`):

- без `DATABASE_URL` процесс не стартует и перечисляет недостающие переменные;
- `OIDC_ISSUER` без `OIDC_CLIENT_ID` — ошибка; без `OIDC_REDIRECT_URL` и
  `PUBLIC_BASE_URL` тоже (callback некуда вести);
- `MATRIX_HOMESERVER` без логина, пароля или pickle-ключа — ошибка;
- `BOT_HANDLER_TIMEOUT` обязан быть длительностью Go (`30s`, `2m`), иначе
  процесс не стартует;
- `REMINDERS_TICK_INTERVAL` обязан быть длительностью Go (`15s`, `1m`), иначе
  процесс не стартует;
- `PUBLIC_BASE_URL` и `OIDC_REDIRECT_URL` обязаны быть абсолютными http(s) URL.

Если транспорт настроен, но `OIDC_ISSUER` нет, транспорт не запускается: без
входа пользователь не сможет привязать аккаунт. В лог пишется предупреждение.
Это же поведение позволяет поднять сервис «только с базой» и проверить
`/readyz` до настройки входа.

## Быстрый старт (Docker Compose)

`deploy/docker-compose.yml` — боевой пример: PostgreSQL 17 и приложение с
healthcheck, `restart: unless-stopped` и двумя томами. Значения берутся из
`deploy/.env`, который Compose читает рядом с файлом.

```bash
cp deploy/.env.example deploy/.env
chmod 600 deploy/.env
$EDITOR deploy/.env            # пароль БД, PUBLIC_BASE_URL, OIDC, токен бота
docker compose -f deploy/docker-compose.yml up -d --build
docker compose -f deploy/docker-compose.yml ps        # оба сервиса healthy
curl -fsS http://127.0.0.1:8080/readyz                # {"status":"ok",…}
```

Что важно в этом файле:

- `db` не публикует порт: до базы дотягивается только `app` по сети compose;
- `app` слушает только `127.0.0.1:${APP_PORT}` — наружу его отдаёт прокси;
- тома `pgdata` (база) и `appdata` (криптосостояние Matrix) переживают
  пересоздание контейнеров; `docker compose down -v` **удаляет** их;
- `app` стартует только после `pg_isready` и сам применяет миграции;
- healthcheck приложения — `GET /readyz` (проверяет базу), поэтому
  `docker compose ps` показывает реальную готовность.

## Обратный прокси и TLS

TLS обязателен: Telegram принимает `web_app`-кнопки только по https, OIDC
провайдеры требуют https-redirect URI, а cookie-сессии получают `Secure` только
при `https://` в `PUBLIC_BASE_URL`.

Пример Caddy (`/etc/caddy/Caddyfile`), TLS-сертификат выпускается
автоматически:

```caddy
cublyverse.example.com {
	encode zstd gzip
	# Mini App, API, MCP и /auth/* — один и тот же origin.
	reverse_proxy 127.0.0.1:8080
}
```

nginx-эквивалент: `proxy_pass http://127.0.0.1:8080;` с `proxy_set_header Host
$host;` и `client_max_body_size 1m;`. WebSocket не нужен: `/mcp` работает по
обычному HTTP (Streamable HTTP), long polling Telegram идёт исходящим
соединением и прокси не требует.

Прокси — единственная публичная точка. Порт приложения и порт базы в интернет
не выставляются.

### Патч-лимитер за прокси

Счётчик неудачных попыток персональных токенов ключуется по адресу TCP-пира
(`r.RemoteAddr`). За прокси это адрес прокси, то есть общий лимит на всех
клиентов. Для одного пользователя это нормально; если понадобится лимит по
клиенту, прокси должен передавать адрес, а сервис — читать доверенный заголовок
(в бэклоге).

## Первый запуск: чек-лист

1. **DNS и TLS.** A/AAAA-запись на сервер, прокси отвечает по https,
   `PUBLIC_BASE_URL` совпадает с адресом без слэша на конце.
2. **Пароль базы.** `openssl rand -base64 32` в `POSTGRES_PASSWORD`
   (`deploy/.env`), файл — `chmod 600`, в git не попадает.
3. **OIDC-клиент** зарегистрирован (раздел ниже): redirect URI
   `PUBLIC_BASE_URL/auth/callback`, scopes `openid profile email`, PKCE.
4. **Telegram-бот** настроен (раздел ниже): токен в `TELEGRAM_BOT_TOKEN`, домен
   Mini App указан в BotFather.
5. **Стек поднят**, `docker compose ps` — оба `healthy`, `/readyz` отвечает
   200.
6. **Вход проверен**: `/auth/login` из браузера даёт cookie-сессию и `/app/`;
   `/api/v1/me` возвращает профиль.
7. **Бот проверен**: `/start` в Telegram присылает кнопку «Войти», привязка
   завершается подтверждением, после неё приходит «✅ Аккаунт привязан».
8. **Напоминание проверено от начала до конца**: создать в WebApp на ближайшую
   минуту, получить сообщение, нажать «✅ Выполнено», увидеть «✅ Выполнено:
   <время>».
9. **Бэкап настроен**: `deploy/backup.sh` отработал, дамп восстановлен в
   тестовую базу (раздел «Бэкапы»).
10. **Переключение с n8n** — по чек-листу
    [MIGRATION_FROM_N8N.md](MIGRATION_FROM_N8N.md), если сервис заменяет старый
    воркфлоу.

## OIDC-клиент

В провайдере (Keycloak, Authentik, Authelia, Google, Yandex ID и т. п.):

```text
Тип клиента:          confidential (есть OIDC_CLIENT_SECRET) или public (пусто)
Redirect URI:         PUBLIC_BASE_URL + /auth/callback
                       (или ровно значение OIDC_REDIRECT_URL, если задано)
Grant:                Authorization Code
PKCE:                 S256 (обязателен)
Scopes:               openid profile email
Post logout redirect: не используется, logout локальный
```

- `OIDC_ISSUER` — issuer из discovery-документа провайдера; сервис скачивает
  `/.well-known/openid-configuration` при старте и падает с понятной ошибкой,
  если провайдер недоступен или issuer не совпадает.
- ID token обязан содержать `nonce`; сервис проверяет подпись, issuer,
  audience и nonce.
- Email и имя необязательны: без них профиль покажет только id, а страница
  подтверждения — «аккаунту SSO».
- Секрет клиента нужен только конфиденциальным клиентам. Public-клиент
  работает на PKCE.

## Telegram-бот

1. `@BotFather` → `/newbot` (или существующий бот) → токен вида
   `123456789:AA…` в `TELEGRAM_BOT_TOKEN`. Это же значение проверяет подпись
   `initData` Mini App, поэтому токен должен принадлежать тому боту, который
   открывает WebApp.
2. `@BotFather` → `/setdomain` (Bot Settings → Domain) → домен из
   `PUBLIC_BASE_URL`. Без этого Telegram не откроет `web_app`-кнопку и меню.
3. Команды и кнопка меню публикуются автоматически при старте, если
   `PUBLIC_BASE_URL` начинается с `https://`: `setMyCommands` (`/start`,
   `/help`, `/reminders`, scope `all_private_chats`, язык `ru`) и
   `setChatMenuButton` на `/app/`. Ошибки этих вызовов логируются и не мешают
   боту.
4. Бот обслуживает только личные чаты; сообщения из групп игнорируются.
5. **Один токен — один поллер.** Нельзя одновременно запускать n8n-воркфлоу и
   этот сервис с тем же токеном: Telegram отдаёт `getUpdates` только одному
   получателю. Подробности — в чек-листе переключения.

## Обновление и политика миграций

Миграции **только вперёд**: `goose` применяет новые версии и никогда не
откатывает их автоматически. Правила совместимости — в
[DATABASE.md](DATABASE.md). Порядок обновления:

```bash
deploy/backup.sh                                   # 1. дамп до обновления
docker compose -f deploy/docker-compose.yml pull   # 2. новый образ (CUBLYVERSE_TAG)
docker compose -f deploy/docker-compose.yml up -d  # 3. пересоздание контейнера
docker compose -f deploy/docker-compose.yml ps     # 4. healthy
curl -fsS http://127.0.0.1:8080/readyz             # 5. база отвечает
```

- Миграции применяет `serve` при старте; вручную —
  `docker compose exec app cublyverse migrate status` (показывает версии и
  неприменённые файлы).
- Одна миграция — один короткий шаг: новые таблицы, nullable-колонки, индексы.
  Долгие операции (`CREATE INDEX CONCURRENTLY`) выполняются отдельно и не
  блокируют работу.
- Перед обновлением, которое меняет схему, бэкап обязателен: откат схемы
  делается восстановлением дампа, а не Down-миграцией.

### Откат

1. Вернуть предыдущий тег образа в `CUBLYVERSE_TAG` и выполнить `up -d`.
   Пока в базе только добавляющие изменения, старый код работает с новой
   схемой: он просто не использует новые колонки и таблицы.
2. Если миграция уже удалила или переименовала что-то, чего старый код ждёт,
   восстановить дамп, сделанный перед обновлением (раздел «Бэкапы»), и только
   потом откатывать образ.
3. После отката проверить `/readyz`, вход в WebApp и одно напоминание.

## Бэкапы

Коротко: `deploy/backup.sh` делает `pg_dump --format=custom` внутри контейнера
базы, пишет дамп в `deploy/backups/`, проверяет размер и удаляет дампы старше
`BACKUP_RETENTION_DAYS` дней.

```bash
deploy/backup.sh
# backup written: ./backups/cublyverse-20260116T031700Z.dump (183421 bytes)
```

Cron на хосте (ежедневно в 03:17):

```cron
17 3 * * * cd /srv/cublyverse && deploy/backup.sh >> /var/log/cublyverse-backup.log 2>&1
```

### Восстановление

Проверенная процедура (выполнялась на одноразовом `postgres:17`):

```bash
# 1. Чистая база для проверки или для восстановления после аварии.
docker run -d --name cublyverse-restore \
  -e POSTGRES_USER=cublyverse -e POSTGRES_PASSWORD=restore-test \
  -e POSTGRES_DB=cublyverse_restore postgres:17
# дождаться готовности: docker exec cublyverse-restore pg_isready -U cublyverse

# 2. Развернуть дамп. pg_restore уже есть в контейнере, клиент на хосте не нужен.
docker exec -i cublyverse-restore \
  pg_restore --no-owner --username cublyverse --dbname cublyverse_restore \
  < deploy/backups/cublyverse-20260116T031700Z.dump

# 3. Убедиться, что схема и данные на месте.
docker exec cublyverse-restore psql -U cublyverse -d cublyverse_restore \
  -tAc "select count(*) from goose_db_version"
docker exec cublyverse-restore psql -U cublyverse -d cublyverse_restore \
  -tAc "select count(*) from reminders"

# 4. Одноразовый контейнер удалить.
docker rm -f cublyverse-restore
```

Восстановление поверх рабочей базы — отдельное осознанное решение: остановите
приложение (`docker compose stop app`), сделайте `pg_dump` текущего состояния,
затем `pg_restore --clean --if-exists --no-owner` в ту же базу и запустите
приложение снова. Восстановленный дамп уже содержит таблицу версий `goose`,
поэтому `serve` не применит миграции повторно.

Секретов в дампе нет: токены сессий и PAT, state и confirm-токены хранятся как
sha256, `initData` не сохраняется. Но дамп содержит тексты напоминаний,
привязки и e-mail — обращайтесь с ним как с персональными данными: шифруйте
диск, ограничьте доступ к `deploy/backups/` (`chmod 700`).

## Данные Matrix на диске

`MATRIX_CRYPTO_DB` — SQLite с криптосостоянием устройства (ключи Olm/Megolm). В
compose он указывает на `/data/matrix-crypto.db`, а `/data` — том `appdata`:

- при пересоздании контейнера ключи остаются, история расшифровывается;
- при потере файла устройство считается новым: старые зашифрованные сообщения
  прочитать нельзя, придётся заново пригласить бота в комнаты;
- `MATRIX_PICKLE_KEY` — ключ шифрования этого файла. Менять его нельзя без
  переинициализации устройства, хранить нужно вместе с бэкапом тома;
- бэкап тома (`docker run --rm -v cublyverse_appdata:/data … tar`) делается
  вместе с бэкапом базы.

## Наблюдаемость

**Логи.** `LOG_FORMAT=json` в проде, одна строка — одно событие. Что искать:

- `http server listening` / `shutdown requested` / `messenger runtime started`
  — жизненный цикл;
- `request_id`, `method`, `path`, `status`, `duration_ms`, `remote_addr` — по
  одному событию на HTTP-запрос (в том числе на пробы `/readyz` от Docker, по
  ним удобно видеть, что healthcheck ходит);
- `user_id` в событиях входа и привязки, `reminder_id` и `occurrence_id` в
  событиях планировщика;
- `reminder delivery failed` (с `last_error`), `reminders tick failed`,
  `reminders scheduler has no messenger delivery` — проблемы доставки;
- `attach imported reminders failed` — импорт из n8n не привязался к
  пользователю; строки остаются в ожидании, повторная привязка или повторный
  `import-n8n` их подхватят;
- `telegram initData verification failed`, `resolve messenger binding failed` —
  проблемы входа;
- ошибки `setMyCommands`/`setChatMenuButton` при старте — обычно неверный
  токен или http-адрес.

**Пробы.**

| Путь | Что проверяет | Ответ |
|---|---|---|
| `GET /healthz` | процесс жив, базу не трогает | `200 {"status":"ok"}` |
| `GET /readyz` | база отвечает (`SELECT 1` через пул) | `200` или `503` |

`/healthz` — для liveness-пробы оркестратора, `/readyz` — для readiness: при
недоступной базе приложение должно быть выведено из балансировки, а не
перезапущено.

**Что мониторить.** Доступность `/readyz`, рост `reminders.last_error`
(ошибки доставки), количество строк `auth_pending` (застрявшие входы),
возраст последнего дампа, свободное место на томе `pgdata` и на диске с
дампами.

## Безопасность

- **TLS только на прокси**, приложение слушает loopback. `PUBLIC_BASE_URL`
  обязан быть `https://`: от него зависит `Secure` у cookie.
- **Секреты только в окружении** (`deploy/.env`, `chmod 600`, не в git; в
  репозитории — `.env.example` с пустыми значениями). Ротация токена бота или
  client secret — это правка `.env` и `up -d`.
- **База не публикуется.** `DATABASE_URL` без `sslmode=disable` вне compose
  (в compose сеть внутренняя); пароль — случайный, не словарный.
- **Сессии и токены.** Cookie — `HttpOnly`, `SameSite=Lax`, изменяющие запросы
  требуют `Content-Type: application/json`; Bearer Mini App живёт в памяти.
  В базе только sha256 токенов, PAT отзывается мгновенно и ограничен областями
  (`profile:read`, `reminders:read`, `reminders:write`).
- **Привязка аккаунта** требует подтверждения на отдельной странице и
  сверки Telegram `initData`: ссылка входа не привязывает чужой аккаунт.
- **Меньше прав.** Контейнер работает не от root (uid 10001). Если нужен
  отдельный пользователь PostgreSQL — создайте его и выдайте права только на
  базу `cublyverse`; приложение не требует суперпользователя.
- **Дампы** содержат персональные данные: шифруйте диск и ограничьте доступ к
  `deploy/backups/`.
- **Один поллер на токен.** Параллельный n8n-воркфлоу с тем же токеном опасен
  не только конфликтом `getUpdates`, но и тем, что два бота отвечают на одни и
  те же нажатия.

## Диагностика

- `403` на `PATCH /me` из браузера — клиент не отправил
  `Content-Type: application/json` (CSRF-правило, см. `docs/API.md`).
- `503 unavailable` на `POST /api/v1/auth/telegram` — не задан
  `TELEGRAM_BOT_TOKEN`.
- `503` на `/readyz`, а контейнер базы `healthy` — неверный `DATABASE_URL` или
  база ещё принимает только локальные подключения; смотрите логи `app`.
- Страница `/auth/login` пишет «Откройте эту страницу из чата с ботом» — ссылка
  открыта в браузере, а не в Mini App; `initData` без Telegram недоступен.
- `410` на странице входа — ссылка старше 10 минут или уже использована:
  начните вход заново из чата.
- Бот молчит, в логах `Conflict: terminated by other getUpdates request` —
  работает второй поллер с тем же токеном (старый n8n-воркфлоу или вторая
  копия сервиса).
- Кнопка меню Telegram не открывается — `PUBLIC_BASE_URL` не https или домен не
  указан в BotFather (`/setdomain`).
- Напоминания не приходят — проверьте `reminders_tick_interval`, строки с
  `last_error` и что конфигурация бота непустая (без транспорта планировщик
  пишет предупреждение и ничего не отправляет).

## Переключение с n8n

Пошаговый чек-лист, включая импорт старых напоминаний, — в
[MIGRATION_FROM_N8N.md](MIGRATION_FROM_N8N.md).
