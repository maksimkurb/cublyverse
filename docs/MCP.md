# MCP

MCP (Model Context Protocol) — вход Cublyverse для LLM-клиентов: Claude Code,
Claude Desktop и любого другого клиента, который умеет Streamable HTTP. Через
него модель читает и меняет напоминания того же пользователя, что и в боте и
WebApp: MCP — третий тонкий адаптер над `internal/features/reminders.Service`, а
не отдельная реализация правил.

## Эндпоинт

```text
POST https://<хост>/mcp
```

В локальной разработке без TLS это `http://localhost:8080/mcp`.

- Транспорт — **Streamable HTTP**, версия протокола — та, которую предложит
  клиент при `initialize`.
- Сервер **stateless**: каждый JSON-RPC запрос приходит отдельным HTTP-запросом,
  состояние между вызовами не хранится, отдельный SSE-поток (`GET /mcp`) не
  поддерживается и отвечает `405`. Так решается несколько задач сразу: рестарт
  процесса или переключение реплики не рвёт сессию клиента, не нужны sticky
  sessions и общий стор, а `Mcp-Session-Id` не нужно ни выдавать, ни проверять.
  Клиенты, которым нужен только запрос-ответ (все инструменты Cublyverse такие),
  работают с этим режимом без настроек.
- Ответы по умолчанию идут как `text/event-stream` (обычный ответ одним
  событием), как того требует спецификация; клиент обязан присылать
  `Accept: application/json, text/event-stream`.
- Защита от DNS rebinding в SDK отключена (`DisableLocalhostProtection`):
  сервис обычно стоит за reverse proxy, который подключается по loopback, а
  публичный `Host` — нет. PAT не является ambient-credential (в отличие от
  cookie), поэтому rebinding не даёт атакующему ничего.

## Авторизация

MCP принимает **только персональные токены**:

```http
Authorization: Bearer cv_pat_…
```

Токен создаётся в WebApp в профиле, в блоке «Токены и MCP» (`docs/WEBAPP.md`) или
через `POST /api/v1/tokens` из сессии (`docs/API.md`). Сервер хранит только
sha256 токена, поэтому показанный один раз секрет восстановить нельзя.

Почему не сессия и не cookie:

- MCP-клиент — не браузер: у него нет cookie-сессии и нет интерактивного входа.
  Принимать cookie на машинном эндпоинте значило бы принимать ambient
  credential, который браузер прикладывает сам; это лишняя поверхность для CSRF
  и путаница в правах.
- PAT ограничен областями доступа (scopes), его можно отозвать в любой момент и
  он принадлежит конкретному пользователю. Cookie-сессия живёт 30 дней и даёт
  полные права.
- Один токен — один клиент: в списке токенов видно, какой из них используется
  (`last_used_at`), и отзыв одного не мешает остальным.

### OAuth 2.1 (вход без ручного токена)

Если задан `PUBLIC_BASE_URL`, Cublyverse сам работает как OAuth authorization
server для MCP-клиентов (`internal/oauthserver`). Пользователя он узнаёт по
обычной SSO-сессии, поэтому провайдеру (Authelia и т. п.) не нужна динамическая
регистрация клиентов. Клиент находит всё сам:

```text
POST /mcp                                               → 401 + WWW-Authenticate: Bearer resource_metadata="…"
GET  /.well-known/oauth-protected-resource/mcp          (RFC 9728)
GET  /.well-known/oauth-authorization-server            (RFC 8414)
POST /oauth/register                                    (RFC 7591, динамическая регистрация)
GET  /oauth/authorize → вход через SSO → экран согласия → redirect с кодом
POST /oauth/token     (authorization_code + PKCE S256, refresh_token)
POST /oauth/revoke    (RFC 7009)
```

- **Регистрация клиента:** динамическая (`/oauth/register`) или по URL
  документа метаданных клиента (CIMD): `client_id` — это `https://…`, сервер
  скачивает документ (только с публичных адресов, без редиректов, до 32 КБ) и
  кэширует его на сутки. Клиенты только публичные, защита — PKCE (S256) и
  точное совпадение `redirect_uri` (для `http://localhost` порт любой).
- **Токены:** непрозрачные, `cv_oat_…` (access, 1 час) и `cv_ort_…` (refresh,
  30 дней, ротация; повторное использование погашенного refresh-токена или кода
  отзывает всю цепочку). В БД хранятся только sha256.
- **Привязка к ресурсу (RFC 8707):** ресурс один — `/mcp`. Доступ действует
  на все пространства пользователя в пределах областей, подтверждённых на экране
  согласия.
- Токены PAT продолжают работать на тех же адресах.
- Отозвать доступ приложения можно в WebApp («Профиль» → «Подключённые приложения»), через `DELETE /api/v1/connections/{id}` или `POST /oauth/revoke`.
- Раз в час фоновая задача `oauth.cleanup` удаляет просроченные коды и токены и динамически зарегистрированных клиентов, которые за неделю так и не получили доступ.

## Эндпоинт и пространства

Все инструменты живут на одном адресе `/mcp`. Каждый мини-апп хранит данные в
пространствах, поэтому инструменты принимают аргумент `space` — id или название
пространства. Список с участниками отдаёт `spaces_list`.

- Склад (`craft_*`): `space` можно не указывать, если склад один; иначе ошибка
  перечисляет доступные.
- Напоминания: `reminders_list` без `space` показывает все пространства,
  `reminders_create` без `space` пишет в личное, `reminders_update` без `space`
  не переносит напоминание. Получатели задаются `recipient_ids` (id участников
  из `spaces_list`).

Чужое и несуществующее пространство неотличимы.

### Области доступа

| Scope | Что разрешает |
|---|---|
| `profile:read` | `whoami`, `spaces_list`; выдается неявно, если у токена есть любой другой scope |
| `reminders:read` | `reminders_list`, `reminders_get` |
| `reminders:write` | `reminders_create`, `reminders_update`, `reminders_delete`, `reminders_complete` |
| `craft:read` | чтение каталога, запасов, закупок и проектов складов (инструменты `craft_*`) |
| `craft:write` | изменение каталога, запасов, закупок и проектов складов |

Неизвестный, просроченный или отозванный токен → `401` (тот же ответ для всех
трёх случаев, чтобы нельзя было перебором отличить причину). Недостаточная
область → ошибка инструмента `insufficient scope: this tool requires …`, которую
модель видит как текст и может объяснить пользователю.

Токен не может управлять токенами и менять профиль: `GET/POST /tokens`,
`DELETE /tokens/{id}` и `PATCH /me` отвечают `403` (см. `docs/API.md`).

### Базовая защита от перебора

Неудачные попытки аутентификации по PAT ограничены в памяти процесса: на каждый
remote IP — корзина на 20 неудачных попыток, восстановление по одной за 2
секунды. Успешный запрос корзину очищает. Это защищает базу от «распыления»
токенов, а не от целенаправленной атаки: полноценный rate limiting ставится на
reverse proxy (в бэклоге — `docs/PLAN.md` §7).

`last_used_at` пишется не чаще раза в минуту на токен: иначе каждый вызов
инструмента превращал бы чтение при аутентификации в запись.

## Инструменты

<!-- BEGIN GENERATED MCP TOOLS -->

| Инструмент | Название | Доступ |
|---|---|---|
| `craft_catalog_search` | Search the catalogue | только чтение |
| `craft_catalogs_set` | Connect official catalogues | изменяет |
| `craft_category_delete` | Delete a category | изменяет и удаляет |
| `craft_category_save` | Create or change a category | изменяет |
| `craft_inventory_list` | List stock | только чтение |
| `craft_item_color_set` | Record the colour of an item | изменяет |
| `craft_item_create` | Create a private item | изменяет |
| `craft_item_delete` | Delete a private item | изменяет и удаляет |
| `craft_item_link` | Link a private item to the catalogue | изменяет и удаляет |
| `craft_item_update` | Update a private item | изменяет |
| `craft_manufacturer_delete` | Delete a manufacturer | изменяет и удаляет |
| `craft_manufacturer_save` | Create or change a manufacturer | изменяет |
| `craft_project_create` | Create a project | изменяет |
| `craft_project_get` | Get a project | только чтение |
| `craft_project_list` | List projects | только чтение |
| `craft_project_set_material` | Plan a material for a project | изменяет |
| `craft_project_shortage_to_wishlist` | Add the project shortage to the wish list | изменяет |
| `craft_project_use_material` | Use material for a project | изменяет |
| `craft_reference` | Warehouse reference data | только чтение |
| `craft_stock_adjust` | Change stock by a delta | изменяет |
| `craft_stock_remove` | Remove an item from stock | изменяет и удаляет |
| `craft_stock_set` | Set stock to a value | изменяет |
| `craft_unit_delete` | Delete a unit | изменяет и удаляет |
| `craft_unit_save` | Create or rename a unit | изменяет |
| `craft_wishlist_add` | Add a wish | изменяет |
| `craft_wishlist_link_add` | Add a shop link to a wish | изменяет |
| `craft_wishlist_link_remove` | Remove a shop link from a wish | изменяет и удаляет |
| `craft_wishlist_list` | List the wish list | только чтение |
| `craft_wishlist_mark_bought` | Mark a wish as bought | изменяет |
| `craft_wishlist_remove` | Remove a wish | изменяет и удаляет |
| `craft_wishlist_update` | Update a wish | изменяет |
| `reminders_complete` | Complete a reminder occurrence | изменяет |
| `reminders_create` | Create a reminder | изменяет |
| `reminders_delete` | Delete a reminder | изменяет и удаляет |
| `reminders_get` | Get a reminder | только чтение |
| `reminders_list` | List reminders | только чтение |
| `reminders_update` | Update a reminder | изменяет |
| `spaces_list` | List spaces | только чтение |
| `whoami` | Who am I | только чтение |

### `craft_catalog_search`

Search the catalogue

Search the shared catalogue (manufacturer catalogues such as PRECIOSA or DMC) and the private items of this workspace. Filters are optional; query matches an article prefix or a part of the name or colour. Each result shows how much of it the workspace holds (stock_qty) and its unit.

**Доступ:** только чтение. Инструмент ничего не меняет, его можно вызывать свободно.

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "category": {
      "description": "category slug or name, see craft_reference",
      "type": "string"
    },
    "color": {
      "description": "colour as #RRGGBB: only items with a colour, closest first",
      "type": "string"
    },
    "limit": {
      "description": "page size 1..200, default 50",
      "type": "integer"
    },
    "manufacturer": {
      "description": "manufacturer name, case-insensitive exact match",
      "type": "string"
    },
    "offset": {
      "description": "number of results to skip",
      "type": "integer"
    },
    "only_in_stock": {
      "description": "only items the workspace holds",
      "type": "boolean"
    },
    "query": {
      "description": "article prefix or part of the name or colour name",
      "type": "string"
    },
    "space": {
      "description": "warehouse space id or name (see spaces_list); may be omitted when the user has exactly one",
      "type": "string"
    }
  },
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "items": {
      "description": "matching items",
      "items": {
        "additionalProperties": false,
        "properties": {
          "article": {
            "description": "manufacturer article number",
            "type": "string"
          },
          "attrs": {
            "additionalProperties": true,
            "description": "category specific attributes",
            "type": "object"
          },
          "category": {
            "description": "category slug",
            "type": "string"
          },
          "category_name": {
            "description": "category name in Russian",
            "type": "string"
          },
          "color_hex": {
            "description": "colour as #RRGGBB",
            "type": "string"
          },
          "color_name": {
            "description": "colour name",
            "type": "string"
          },
          "color_source": {
            "description": "how trustworthy color_hex is: official, approx, or photo (computed from the product photo, rough)",
            "type": "string"
          },
          "finish": {
            "description": "finish or coating, free text",
            "type": "string"
          },
          "id": {
            "description": "catalogue item id (UUID)",
            "type": "string"
          },
          "image_url": {
            "description": "link to a photo of the item",
            "type": "string"
          },
          "in_inventory": {
            "description": "whether the workspace has a stock row for the item",
            "type": "boolean"
          },
          "manufacturer": {
            "description": "manufacturer name",
            "type": "string"
          },
          "name": {
            "description": "item name",
            "type": "string"
          },
          "pack_size": {
            "description": "how many base units one pack holds, when known",
            "type": [
              "null",
              "number"
            ]
          },
          "private": {
            "description": "true for an item private to this workspace, false for a global catalogue item",
            "type": "boolean"
          },
          "series": {
            "description": "series or variety",
            "type": "string"
          },
          "size": {
            "description": "size, free text",
            "type": "string"
          },
          "stock_qty": {
            "description": "quantity this workspace holds, in unit",
            "type": "number"
          },
          "unit": {
            "description": "unit the quantity is counted in",
            "type": "string"
          }
        },
        "required": [
          "id",
          "private",
          "category",
          "category_name",
          "name",
          "unit",
          "stock_qty",
          "in_inventory"
        ],
        "type": "object"
      },
      "type": [
        "null",
        "array"
      ]
    }
  },
  "required": [
    "items"
  ],
  "type": "object"
}
```

### `craft_catalogs_set`

Connect official catalogues

Replace the set of official catalogues the warehouse searches (see craft_reference for the available sources). Stock already held stays when a catalogue is disconnected.

**Доступ:** изменяет. Повторный вызов не создаёт нового состояния; ответ может отличаться (например, «уже подтверждено»).

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "sources": {
      "description": "catalogue keys to keep connected; empty disconnects all",
      "items": {
        "type": "string"
      },
      "type": [
        "null",
        "array"
      ]
    },
    "space": {
      "description": "warehouse space id or name (see spaces_list); may be omitted when the user has exactly one",
      "type": "string"
    }
  },
  "required": [
    "sources"
  ],
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "catalogs": {
      "description": "official catalogues",
      "items": {
        "additionalProperties": false,
        "properties": {
          "enabled": {
            "description": "connected to this warehouse",
            "type": "boolean"
          },
          "items": {
            "description": "number of positions",
            "type": "integer"
          },
          "name": {
            "description": "catalogue name",
            "type": "string"
          },
          "source": {
            "description": "catalogue key for craft_catalogs_set",
            "type": "string"
          }
        },
        "required": [
          "source",
          "name",
          "enabled",
          "items"
        ],
        "type": "object"
      },
      "type": [
        "null",
        "array"
      ]
    }
  },
  "required": [
    "catalogs"
  ],
  "type": "object"
}
```

### `craft_category_delete`

Delete a category

Delete a category that none of the warehouse's own items use.

**Доступ:** изменяет и удаляет. Вызов может удалить данные; клиент должен подтверждать его у пользователя.

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "id": {
      "description": "id from craft_reference",
      "type": "string"
    },
    "space": {
      "description": "warehouse space id or name (see spaces_list); may be omitted when the user has exactly one",
      "type": "string"
    }
  },
  "required": [
    "id"
  ],
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "deleted": {
      "description": "always true on success",
      "type": "boolean"
    },
    "id": {
      "description": "deleted id",
      "type": "string"
    }
  },
  "required": [
    "id",
    "deleted"
  ],
  "type": "object"
}
```

### `craft_category_save`

Create or change a category

Create a category (omit id) or change one. default_unit must be a unit of the warehouse. fields defines extra values items of the category carry in attrs; on a change, omitted name, default_unit or fields keep their current values.

**Доступ:** изменяет. Каждый вызов создаёт новое состояние.

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "default_unit": {
      "description": "a unit of the warehouse",
      "type": "string"
    },
    "fields": {
      "description": "all extra fields of the category; replaces the current list",
      "items": {
        "additionalProperties": false,
        "properties": {
          "key": {
            "description": "name of the value in item attrs; omit for a new field",
            "type": "string"
          },
          "label": {
            "description": "label shown to people",
            "type": "string"
          },
          "options": {
            "description": "allowed values of a select field",
            "items": {
              "type": "string"
            },
            "type": [
              "null",
              "array"
            ]
          },
          "type": {
            "description": "text, number, bool or select",
            "type": "string"
          }
        },
        "required": [
          "label",
          "type"
        ],
        "type": "object"
      },
      "type": [
        "null",
        "array"
      ]
    },
    "id": {
      "description": "category to change; omit to create",
      "type": "string"
    },
    "name": {
      "description": "category name",
      "type": "string"
    },
    "space": {
      "description": "warehouse space id or name (see spaces_list); may be omitted when the user has exactly one",
      "type": "string"
    }
  },
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "default_unit": {
      "description": "unit used when an item does not name one",
      "type": "string"
    },
    "fields": {
      "description": "extra fields; their values go to item attrs under key",
      "items": {
        "additionalProperties": false,
        "properties": {
          "key": {
            "description": "name of the value in item attrs; omit for a new field",
            "type": "string"
          },
          "label": {
            "description": "label shown to people",
            "type": "string"
          },
          "options": {
            "description": "allowed values of a select field",
            "items": {
              "type": "string"
            },
            "type": [
              "null",
              "array"
            ]
          },
          "type": {
            "description": "text, number, bool or select",
            "type": "string"
          }
        },
        "required": [
          "label",
          "type"
        ],
        "type": "object"
      },
      "type": [
        "null",
        "array"
      ]
    },
    "id": {
      "description": "category id",
      "type": "string"
    },
    "name": {
      "description": "category name",
      "type": "string"
    },
    "own_items": {
      "description": "how many of the warehouse's own items use it",
      "type": "integer"
    },
    "slug": {
      "description": "category slug to use in filters and item data (a name works too)",
      "type": "string"
    }
  },
  "required": [
    "id",
    "slug",
    "name",
    "default_unit",
    "fields",
    "own_items"
  ],
  "type": "object"
}
```

### `craft_inventory_list`

List stock

List what the workspace holds: items with quantity and unit, optionally filtered by category, manufacturer or text. Use max_qty to find items that are running low.

**Доступ:** только чтение. Инструмент ничего не меняет, его можно вызывать свободно.

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "category": {
      "description": "category slug or name, see craft_reference",
      "type": "string"
    },
    "color": {
      "description": "colour as #RRGGBB: only items with a colour, closest first",
      "type": "string"
    },
    "limit": {
      "description": "page size 1..200, default 50",
      "type": "integer"
    },
    "manufacturer": {
      "description": "manufacturer name, case-insensitive exact match",
      "type": "string"
    },
    "max_qty": {
      "description": "only rows with quantity at most this value, to find low stock",
      "type": [
        "null",
        "number"
      ]
    },
    "offset": {
      "description": "number of results to skip",
      "type": "integer"
    },
    "query": {
      "description": "article prefix or part of the name or colour name",
      "type": "string"
    },
    "space": {
      "description": "warehouse space id or name (see spaces_list); may be omitted when the user has exactly one",
      "type": "string"
    }
  },
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "stock": {
      "description": "stock rows",
      "items": {
        "additionalProperties": false,
        "properties": {
          "item": {
            "additionalProperties": false,
            "description": "the catalogue item",
            "properties": {
              "article": {
                "description": "manufacturer article number",
                "type": "string"
              },
              "attrs": {
                "additionalProperties": true,
                "description": "category specific attributes",
                "type": "object"
              },
              "category": {
                "description": "category slug",
                "type": "string"
              },
              "category_name": {
                "description": "category name in Russian",
                "type": "string"
              },
              "color_hex": {
                "description": "colour as #RRGGBB",
                "type": "string"
              },
              "color_name": {
                "description": "colour name",
                "type": "string"
              },
              "color_source": {
                "description": "how trustworthy color_hex is: official, approx, or photo (computed from the product photo, rough)",
                "type": "string"
              },
              "finish": {
                "description": "finish or coating, free text",
                "type": "string"
              },
              "id": {
                "description": "catalogue item id (UUID)",
                "type": "string"
              },
              "image_url": {
                "description": "link to a photo of the item",
                "type": "string"
              },
              "in_inventory": {
                "description": "whether the workspace has a stock row for the item",
                "type": "boolean"
              },
              "manufacturer": {
                "description": "manufacturer name",
                "type": "string"
              },
              "name": {
                "description": "item name",
                "type": "string"
              },
              "pack_size": {
                "description": "how many base units one pack holds, when known",
                "type": [
                  "null",
                  "number"
                ]
              },
              "private": {
                "description": "true for an item private to this workspace, false for a global catalogue item",
                "type": "boolean"
              },
              "series": {
                "description": "series or variety",
                "type": "string"
              },
              "size": {
                "description": "size, free text",
                "type": "string"
              },
              "stock_qty": {
                "description": "quantity this workspace holds, in unit",
                "type": "number"
              },
              "unit": {
                "description": "unit the quantity is counted in",
                "type": "string"
              }
            },
            "required": [
              "id",
              "private",
              "category",
              "category_name",
              "name",
              "unit",
              "stock_qty",
              "in_inventory"
            ],
            "type": "object"
          },
          "location": {
            "description": "where it is stored",
            "type": "string"
          },
          "notes": {
            "description": "notes about this stock",
            "type": "string"
          },
          "qty": {
            "description": "quantity held, in item.unit",
            "type": "number"
          },
          "updated_at": {
            "description": "last change in RFC 3339 UTC",
            "type": "string"
          }
        },
        "required": [
          "item",
          "qty",
          "updated_at"
        ],
        "type": "object"
      },
      "type": [
        "null",
        "array"
      ]
    }
  },
  "required": [
    "stock"
  ],
  "type": "object"
}
```

### `craft_item_color_set`

Record the colour of an item

Record an approximate colour of an item with the source it comes from. An own item is changed itself. An official item is read-only: the colour is kept with this warehouse's stock (the item must be in stock) and is shown only while the official catalogue has no colour. Never invent a value: give the page or method it came from.

**Доступ:** изменяет. Повторный вызов не создаёт нового состояния; ответ может отличаться (например, «уже подтверждено»).

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "color_hex": {
      "description": "colour as #RRGGBB",
      "type": "string"
    },
    "color_name": {
      "description": "colour name; omit to keep the current one",
      "type": "string"
    },
    "item_id": {
      "description": "catalogue item id (UUID)",
      "type": "string"
    },
    "source": {
      "description": "where the colour comes from: a page address or the method, up to 500 characters",
      "type": "string"
    },
    "space": {
      "description": "warehouse space id or name (see spaces_list); may be omitted when the user has exactly one",
      "type": "string"
    }
  },
  "required": [
    "item_id",
    "color_hex",
    "source"
  ],
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "article": {
      "description": "manufacturer article number",
      "type": "string"
    },
    "attrs": {
      "additionalProperties": true,
      "description": "category specific attributes",
      "type": "object"
    },
    "category": {
      "description": "category slug",
      "type": "string"
    },
    "category_name": {
      "description": "category name in Russian",
      "type": "string"
    },
    "color_hex": {
      "description": "colour as #RRGGBB",
      "type": "string"
    },
    "color_name": {
      "description": "colour name",
      "type": "string"
    },
    "color_source": {
      "description": "how trustworthy color_hex is: official, approx, or photo (computed from the product photo, rough)",
      "type": "string"
    },
    "finish": {
      "description": "finish or coating, free text",
      "type": "string"
    },
    "id": {
      "description": "catalogue item id (UUID)",
      "type": "string"
    },
    "image_url": {
      "description": "link to a photo of the item",
      "type": "string"
    },
    "in_inventory": {
      "description": "whether the workspace has a stock row for the item",
      "type": "boolean"
    },
    "manufacturer": {
      "description": "manufacturer name",
      "type": "string"
    },
    "name": {
      "description": "item name",
      "type": "string"
    },
    "pack_size": {
      "description": "how many base units one pack holds, when known",
      "type": [
        "null",
        "number"
      ]
    },
    "private": {
      "description": "true for an item private to this workspace, false for a global catalogue item",
      "type": "boolean"
    },
    "series": {
      "description": "series or variety",
      "type": "string"
    },
    "size": {
      "description": "size, free text",
      "type": "string"
    },
    "stock_qty": {
      "description": "quantity this workspace holds, in unit",
      "type": "number"
    },
    "unit": {
      "description": "unit the quantity is counted in",
      "type": "string"
    }
  },
  "required": [
    "id",
    "private",
    "category",
    "category_name",
    "name",
    "unit",
    "stock_qty",
    "in_inventory"
  ],
  "type": "object"
}
```

### `craft_item_create`

Create a private item

Create a catalogue item private to this workspace, for material that is in no manufacturer catalogue (for example a thread of Mallorca pearls). The unit defaults to the category's default unit. It can be linked to an official catalogue item later with craft_item_link.

**Доступ:** изменяет. Каждый вызов создаёт новое состояние.

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "article": {
      "description": "article number",
      "type": "string"
    },
    "attrs": {
      "additionalProperties": true,
      "description": "values of the category's extra fields by key, see craft_reference",
      "type": "object"
    },
    "category": {
      "description": "category slug or name, see craft_reference",
      "type": "string"
    },
    "color_hex": {
      "description": "colour as #RRGGBB",
      "type": "string"
    },
    "color_name": {
      "description": "colour name",
      "type": "string"
    },
    "color_source": {
      "description": "official or approx; approx when omitted and color_hex is set",
      "type": "string"
    },
    "finish": {
      "description": "finish or coating, free text",
      "type": "string"
    },
    "image_url": {
      "description": "http(s) link to a photo of the item",
      "type": "string"
    },
    "manufacturer": {
      "description": "manufacturer name; created in the warehouse when unknown",
      "type": "string"
    },
    "name": {
      "description": "item name; defaults to manufacturer + article",
      "type": "string"
    },
    "pack_size": {
      "description": "how many base units one pack holds",
      "type": [
        "null",
        "number"
      ]
    },
    "series": {
      "description": "series or variety",
      "type": "string"
    },
    "size": {
      "description": "size, free text",
      "type": "string"
    },
    "space": {
      "description": "warehouse space id or name (see spaces_list); may be omitted when the user has exactly one",
      "type": "string"
    },
    "unit": {
      "description": "unit; the category default when omitted",
      "type": "string"
    }
  },
  "required": [
    "category"
  ],
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "article": {
      "description": "manufacturer article number",
      "type": "string"
    },
    "attrs": {
      "additionalProperties": true,
      "description": "category specific attributes",
      "type": "object"
    },
    "category": {
      "description": "category slug",
      "type": "string"
    },
    "category_name": {
      "description": "category name in Russian",
      "type": "string"
    },
    "color_hex": {
      "description": "colour as #RRGGBB",
      "type": "string"
    },
    "color_name": {
      "description": "colour name",
      "type": "string"
    },
    "color_source": {
      "description": "how trustworthy color_hex is: official, approx, or photo (computed from the product photo, rough)",
      "type": "string"
    },
    "finish": {
      "description": "finish or coating, free text",
      "type": "string"
    },
    "id": {
      "description": "catalogue item id (UUID)",
      "type": "string"
    },
    "image_url": {
      "description": "link to a photo of the item",
      "type": "string"
    },
    "in_inventory": {
      "description": "whether the workspace has a stock row for the item",
      "type": "boolean"
    },
    "manufacturer": {
      "description": "manufacturer name",
      "type": "string"
    },
    "name": {
      "description": "item name",
      "type": "string"
    },
    "pack_size": {
      "description": "how many base units one pack holds, when known",
      "type": [
        "null",
        "number"
      ]
    },
    "private": {
      "description": "true for an item private to this workspace, false for a global catalogue item",
      "type": "boolean"
    },
    "series": {
      "description": "series or variety",
      "type": "string"
    },
    "size": {
      "description": "size, free text",
      "type": "string"
    },
    "stock_qty": {
      "description": "quantity this workspace holds, in unit",
      "type": "number"
    },
    "unit": {
      "description": "unit the quantity is counted in",
      "type": "string"
    }
  },
  "required": [
    "id",
    "private",
    "category",
    "category_name",
    "name",
    "unit",
    "stock_qty",
    "in_inventory"
  ],
  "type": "object"
}
```

### `craft_item_delete`

Delete a private item

Delete a private item together with its stock. Global catalogue items cannot be deleted.

**Доступ:** изменяет и удаляет. Вызов может удалить данные; клиент должен подтверждать его у пользователя.

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "item_id": {
      "description": "catalogue item id (UUID)",
      "type": "string"
    },
    "space": {
      "description": "warehouse space id or name (see spaces_list); may be omitted when the user has exactly one",
      "type": "string"
    }
  },
  "required": [
    "item_id"
  ],
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "item_id": {
      "description": "item whose stock row was removed",
      "type": "string"
    },
    "removed": {
      "description": "always true on success",
      "type": "boolean"
    }
  },
  "required": [
    "item_id",
    "removed"
  ],
  "type": "object"
}
```

### `craft_item_link`

Link a private item to the catalogue

Move the stock of a private item to another catalogue item (usually the official one) and delete the private item. Units must match; quantities are summed when the target is already in stock.

**Доступ:** изменяет и удаляет. Вызов может удалить данные; клиент должен подтверждать его у пользователя.

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "private_item_id": {
      "description": "the private item to merge away",
      "type": "string"
    },
    "space": {
      "description": "warehouse space id or name (see spaces_list); may be omitted when the user has exactly one",
      "type": "string"
    },
    "target_item_id": {
      "description": "the catalogue item that receives its stock",
      "type": "string"
    }
  },
  "required": [
    "private_item_id",
    "target_item_id"
  ],
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "item": {
      "additionalProperties": false,
      "description": "the catalogue item",
      "properties": {
        "article": {
          "description": "manufacturer article number",
          "type": "string"
        },
        "attrs": {
          "additionalProperties": true,
          "description": "category specific attributes",
          "type": "object"
        },
        "category": {
          "description": "category slug",
          "type": "string"
        },
        "category_name": {
          "description": "category name in Russian",
          "type": "string"
        },
        "color_hex": {
          "description": "colour as #RRGGBB",
          "type": "string"
        },
        "color_name": {
          "description": "colour name",
          "type": "string"
        },
        "color_source": {
          "description": "how trustworthy color_hex is: official, approx, or photo (computed from the product photo, rough)",
          "type": "string"
        },
        "finish": {
          "description": "finish or coating, free text",
          "type": "string"
        },
        "id": {
          "description": "catalogue item id (UUID)",
          "type": "string"
        },
        "image_url": {
          "description": "link to a photo of the item",
          "type": "string"
        },
        "in_inventory": {
          "description": "whether the workspace has a stock row for the item",
          "type": "boolean"
        },
        "manufacturer": {
          "description": "manufacturer name",
          "type": "string"
        },
        "name": {
          "description": "item name",
          "type": "string"
        },
        "pack_size": {
          "description": "how many base units one pack holds, when known",
          "type": [
            "null",
            "number"
          ]
        },
        "private": {
          "description": "true for an item private to this workspace, false for a global catalogue item",
          "type": "boolean"
        },
        "series": {
          "description": "series or variety",
          "type": "string"
        },
        "size": {
          "description": "size, free text",
          "type": "string"
        },
        "stock_qty": {
          "description": "quantity this workspace holds, in unit",
          "type": "number"
        },
        "unit": {
          "description": "unit the quantity is counted in",
          "type": "string"
        }
      },
      "required": [
        "id",
        "private",
        "category",
        "category_name",
        "name",
        "unit",
        "stock_qty",
        "in_inventory"
      ],
      "type": "object"
    },
    "location": {
      "description": "where it is stored",
      "type": "string"
    },
    "notes": {
      "description": "notes about this stock",
      "type": "string"
    },
    "qty": {
      "description": "quantity held, in item.unit",
      "type": "number"
    },
    "updated_at": {
      "description": "last change in RFC 3339 UTC",
      "type": "string"
    }
  },
  "required": [
    "item",
    "qty",
    "updated_at"
  ],
  "type": "object"
}
```

### `craft_item_update`

Update a private item

Replace the data of a private item of this workspace. All fields are replaced, so read the item with craft_catalog_search first and send the full data. Global catalogue items cannot be changed.

**Доступ:** изменяет. Повторный вызов не создаёт нового состояния; ответ может отличаться (например, «уже подтверждено»).

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "article": {
      "description": "article number",
      "type": "string"
    },
    "attrs": {
      "additionalProperties": true,
      "description": "values of the category's extra fields by key, see craft_reference",
      "type": "object"
    },
    "category": {
      "description": "category slug or name, see craft_reference",
      "type": "string"
    },
    "color_hex": {
      "description": "colour as #RRGGBB",
      "type": "string"
    },
    "color_name": {
      "description": "colour name",
      "type": "string"
    },
    "color_source": {
      "description": "official or approx; approx when omitted and color_hex is set",
      "type": "string"
    },
    "finish": {
      "description": "finish or coating, free text",
      "type": "string"
    },
    "image_url": {
      "description": "http(s) link to a photo of the item",
      "type": "string"
    },
    "item_id": {
      "description": "id of the private item to replace",
      "type": "string"
    },
    "manufacturer": {
      "description": "manufacturer name; created in the warehouse when unknown",
      "type": "string"
    },
    "name": {
      "description": "item name; defaults to manufacturer + article",
      "type": "string"
    },
    "pack_size": {
      "description": "how many base units one pack holds",
      "type": [
        "null",
        "number"
      ]
    },
    "series": {
      "description": "series or variety",
      "type": "string"
    },
    "size": {
      "description": "size, free text",
      "type": "string"
    },
    "space": {
      "description": "warehouse space id or name (see spaces_list); may be omitted when the user has exactly one",
      "type": "string"
    },
    "unit": {
      "description": "unit; the category default when omitted",
      "type": "string"
    }
  },
  "required": [
    "item_id",
    "category"
  ],
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "article": {
      "description": "manufacturer article number",
      "type": "string"
    },
    "attrs": {
      "additionalProperties": true,
      "description": "category specific attributes",
      "type": "object"
    },
    "category": {
      "description": "category slug",
      "type": "string"
    },
    "category_name": {
      "description": "category name in Russian",
      "type": "string"
    },
    "color_hex": {
      "description": "colour as #RRGGBB",
      "type": "string"
    },
    "color_name": {
      "description": "colour name",
      "type": "string"
    },
    "color_source": {
      "description": "how trustworthy color_hex is: official, approx, or photo (computed from the product photo, rough)",
      "type": "string"
    },
    "finish": {
      "description": "finish or coating, free text",
      "type": "string"
    },
    "id": {
      "description": "catalogue item id (UUID)",
      "type": "string"
    },
    "image_url": {
      "description": "link to a photo of the item",
      "type": "string"
    },
    "in_inventory": {
      "description": "whether the workspace has a stock row for the item",
      "type": "boolean"
    },
    "manufacturer": {
      "description": "manufacturer name",
      "type": "string"
    },
    "name": {
      "description": "item name",
      "type": "string"
    },
    "pack_size": {
      "description": "how many base units one pack holds, when known",
      "type": [
        "null",
        "number"
      ]
    },
    "private": {
      "description": "true for an item private to this workspace, false for a global catalogue item",
      "type": "boolean"
    },
    "series": {
      "description": "series or variety",
      "type": "string"
    },
    "size": {
      "description": "size, free text",
      "type": "string"
    },
    "stock_qty": {
      "description": "quantity this workspace holds, in unit",
      "type": "number"
    },
    "unit": {
      "description": "unit the quantity is counted in",
      "type": "string"
    }
  },
  "required": [
    "id",
    "private",
    "category",
    "category_name",
    "name",
    "unit",
    "stock_qty",
    "in_inventory"
  ],
  "type": "object"
}
```

### `craft_manufacturer_delete`

Delete a manufacturer

Delete an own manufacturer that no item uses.

**Доступ:** изменяет и удаляет. Вызов может удалить данные; клиент должен подтверждать его у пользователя.

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "id": {
      "description": "id from craft_reference",
      "type": "string"
    },
    "space": {
      "description": "warehouse space id or name (see spaces_list); may be omitted when the user has exactly one",
      "type": "string"
    }
  },
  "required": [
    "id"
  ],
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "deleted": {
      "description": "always true on success",
      "type": "boolean"
    },
    "id": {
      "description": "deleted id",
      "type": "string"
    }
  },
  "required": [
    "id",
    "deleted"
  ],
  "type": "object"
}
```

### `craft_manufacturer_save`

Create or change a manufacturer

Create an own manufacturer (omit id) or change one. Manufacturers of the official catalogues are read-only. A change replaces both name and website.

**Доступ:** изменяет. Каждый вызов создаёт новое состояние.

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "id": {
      "description": "manufacturer to change; omit to create",
      "type": "string"
    },
    "name": {
      "description": "manufacturer name",
      "type": "string"
    },
    "space": {
      "description": "warehouse space id or name (see spaces_list); may be omitted when the user has exactly one",
      "type": "string"
    },
    "website": {
      "description": "http(s) website",
      "type": "string"
    }
  },
  "required": [
    "name"
  ],
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "id": {
      "description": "manufacturer id",
      "type": "string"
    },
    "name": {
      "description": "manufacturer name",
      "type": "string"
    },
    "official": {
      "description": "from an official catalogue, read-only",
      "type": "boolean"
    },
    "website": {
      "description": "website",
      "type": "string"
    }
  },
  "required": [
    "id",
    "name",
    "official"
  ],
  "type": "object"
}
```

### `craft_project_create`

Create a project

Create a craft project with a name and optional notes. Materials are added with craft_project_set_material.

**Доступ:** изменяет. Каждый вызов создаёт новое состояние.

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "name": {
      "description": "project name, 1 to 200 characters",
      "type": "string"
    },
    "notes": {
      "description": "free-text notes",
      "type": "string"
    },
    "space": {
      "description": "warehouse space id or name (see spaces_list); may be omitted when the user has exactly one",
      "type": "string"
    }
  },
  "required": [
    "name"
  ],
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "created_at": {
      "description": "creation time in RFC 3339 UTC",
      "type": "string"
    },
    "id": {
      "description": "project id (UUID)",
      "type": "string"
    },
    "material_count": {
      "description": "number of materials planned",
      "type": "integer"
    },
    "materials": {
      "description": "materials; empty when the project is fresh or listed",
      "items": {
        "additionalProperties": false,
        "properties": {
          "item": {
            "additionalProperties": false,
            "description": "the catalogue item with the current stock",
            "properties": {
              "article": {
                "description": "manufacturer article number",
                "type": "string"
              },
              "attrs": {
                "additionalProperties": true,
                "description": "category specific attributes",
                "type": "object"
              },
              "category": {
                "description": "category slug",
                "type": "string"
              },
              "category_name": {
                "description": "category name in Russian",
                "type": "string"
              },
              "color_hex": {
                "description": "colour as #RRGGBB",
                "type": "string"
              },
              "color_name": {
                "description": "colour name",
                "type": "string"
              },
              "color_source": {
                "description": "how trustworthy color_hex is: official, approx, or photo (computed from the product photo, rough)",
                "type": "string"
              },
              "finish": {
                "description": "finish or coating, free text",
                "type": "string"
              },
              "id": {
                "description": "catalogue item id (UUID)",
                "type": "string"
              },
              "image_url": {
                "description": "link to a photo of the item",
                "type": "string"
              },
              "in_inventory": {
                "description": "whether the workspace has a stock row for the item",
                "type": "boolean"
              },
              "manufacturer": {
                "description": "manufacturer name",
                "type": "string"
              },
              "name": {
                "description": "item name",
                "type": "string"
              },
              "pack_size": {
                "description": "how many base units one pack holds, when known",
                "type": [
                  "null",
                  "number"
                ]
              },
              "private": {
                "description": "true for an item private to this workspace, false for a global catalogue item",
                "type": "boolean"
              },
              "series": {
                "description": "series or variety",
                "type": "string"
              },
              "size": {
                "description": "size, free text",
                "type": "string"
              },
              "stock_qty": {
                "description": "quantity this workspace holds, in unit",
                "type": "number"
              },
              "unit": {
                "description": "unit the quantity is counted in",
                "type": "string"
              }
            },
            "required": [
              "id",
              "private",
              "category",
              "category_name",
              "name",
              "unit",
              "stock_qty",
              "in_inventory"
            ],
            "type": "object"
          },
          "qty_planned": {
            "description": "planned quantity, absent when no plan is set",
            "type": [
              "null",
              "number"
            ]
          },
          "qty_used": {
            "description": "quantity already used by the project",
            "type": "number"
          },
          "shortage": {
            "description": "max(0, qty_planned - qty_used - stock): how much is still to buy",
            "type": "number"
          }
        },
        "required": [
          "item",
          "qty_used",
          "shortage"
        ],
        "type": "object"
      },
      "type": [
        "null",
        "array"
      ]
    },
    "name": {
      "description": "project name",
      "type": "string"
    },
    "notes": {
      "description": "free-text notes",
      "type": "string"
    }
  },
  "required": [
    "id",
    "name",
    "material_count",
    "materials",
    "created_at"
  ],
  "type": "object"
}
```

### `craft_project_get`

Get a project

Return one project with its materials: planned quantity, quantity already used, the current stock and the shortage. shortage = max(0, qty_planned - qty_used - stock), the quantity still to buy.

**Доступ:** только чтение. Инструмент ничего не меняет, его можно вызывать свободно.

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "project_id": {
      "description": "project id (UUID)",
      "type": "string"
    },
    "space": {
      "description": "warehouse space id or name (see spaces_list); may be omitted when the user has exactly one",
      "type": "string"
    }
  },
  "required": [
    "project_id"
  ],
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "created_at": {
      "description": "creation time in RFC 3339 UTC",
      "type": "string"
    },
    "id": {
      "description": "project id (UUID)",
      "type": "string"
    },
    "material_count": {
      "description": "number of materials planned",
      "type": "integer"
    },
    "materials": {
      "description": "materials; empty when the project is fresh or listed",
      "items": {
        "additionalProperties": false,
        "properties": {
          "item": {
            "additionalProperties": false,
            "description": "the catalogue item with the current stock",
            "properties": {
              "article": {
                "description": "manufacturer article number",
                "type": "string"
              },
              "attrs": {
                "additionalProperties": true,
                "description": "category specific attributes",
                "type": "object"
              },
              "category": {
                "description": "category slug",
                "type": "string"
              },
              "category_name": {
                "description": "category name in Russian",
                "type": "string"
              },
              "color_hex": {
                "description": "colour as #RRGGBB",
                "type": "string"
              },
              "color_name": {
                "description": "colour name",
                "type": "string"
              },
              "color_source": {
                "description": "how trustworthy color_hex is: official, approx, or photo (computed from the product photo, rough)",
                "type": "string"
              },
              "finish": {
                "description": "finish or coating, free text",
                "type": "string"
              },
              "id": {
                "description": "catalogue item id (UUID)",
                "type": "string"
              },
              "image_url": {
                "description": "link to a photo of the item",
                "type": "string"
              },
              "in_inventory": {
                "description": "whether the workspace has a stock row for the item",
                "type": "boolean"
              },
              "manufacturer": {
                "description": "manufacturer name",
                "type": "string"
              },
              "name": {
                "description": "item name",
                "type": "string"
              },
              "pack_size": {
                "description": "how many base units one pack holds, when known",
                "type": [
                  "null",
                  "number"
                ]
              },
              "private": {
                "description": "true for an item private to this workspace, false for a global catalogue item",
                "type": "boolean"
              },
              "series": {
                "description": "series or variety",
                "type": "string"
              },
              "size": {
                "description": "size, free text",
                "type": "string"
              },
              "stock_qty": {
                "description": "quantity this workspace holds, in unit",
                "type": "number"
              },
              "unit": {
                "description": "unit the quantity is counted in",
                "type": "string"
              }
            },
            "required": [
              "id",
              "private",
              "category",
              "category_name",
              "name",
              "unit",
              "stock_qty",
              "in_inventory"
            ],
            "type": "object"
          },
          "qty_planned": {
            "description": "planned quantity, absent when no plan is set",
            "type": [
              "null",
              "number"
            ]
          },
          "qty_used": {
            "description": "quantity already used by the project",
            "type": "number"
          },
          "shortage": {
            "description": "max(0, qty_planned - qty_used - stock): how much is still to buy",
            "type": "number"
          }
        },
        "required": [
          "item",
          "qty_used",
          "shortage"
        ],
        "type": "object"
      },
      "type": [
        "null",
        "array"
      ]
    },
    "name": {
      "description": "project name",
      "type": "string"
    },
    "notes": {
      "description": "free-text notes",
      "type": "string"
    }
  },
  "required": [
    "id",
    "name",
    "material_count",
    "materials",
    "created_at"
  ],
  "type": "object"
}
```

### `craft_project_list`

List projects

List the craft projects of the workspace with the number of materials planned in each.

**Доступ:** только чтение. Инструмент ничего не меняет, его можно вызывать свободно.

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "space": {
      "description": "warehouse space id or name (see spaces_list); may be omitted when the user has exactly one",
      "type": "string"
    }
  },
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "projects": {
      "description": "projects of the workspace; empty when there are none",
      "items": {
        "additionalProperties": false,
        "properties": {
          "created_at": {
            "description": "creation time in RFC 3339 UTC",
            "type": "string"
          },
          "id": {
            "description": "project id (UUID)",
            "type": "string"
          },
          "material_count": {
            "description": "number of materials planned",
            "type": "integer"
          },
          "materials": {
            "description": "materials; empty when the project is fresh or listed",
            "items": {
              "additionalProperties": false,
              "properties": {
                "item": {
                  "additionalProperties": false,
                  "description": "the catalogue item with the current stock",
                  "properties": {
                    "article": {
                      "description": "manufacturer article number",
                      "type": "string"
                    },
                    "attrs": {
                      "additionalProperties": true,
                      "description": "category specific attributes",
                      "type": "object"
                    },
                    "category": {
                      "description": "category slug",
                      "type": "string"
                    },
                    "category_name": {
                      "description": "category name in Russian",
                      "type": "string"
                    },
                    "color_hex": {
                      "description": "colour as #RRGGBB",
                      "type": "string"
                    },
                    "color_name": {
                      "description": "colour name",
                      "type": "string"
                    },
                    "color_source": {
                      "description": "how trustworthy color_hex is: official, approx, or photo (computed from the product photo, rough)",
                      "type": "string"
                    },
                    "finish": {
                      "description": "finish or coating, free text",
                      "type": "string"
                    },
                    "id": {
                      "description": "catalogue item id (UUID)",
                      "type": "string"
                    },
                    "image_url": {
                      "description": "link to a photo of the item",
                      "type": "string"
                    },
                    "in_inventory": {
                      "description": "whether the workspace has a stock row for the item",
                      "type": "boolean"
                    },
                    "manufacturer": {
                      "description": "manufacturer name",
                      "type": "string"
                    },
                    "name": {
                      "description": "item name",
                      "type": "string"
                    },
                    "pack_size": {
                      "description": "how many base units one pack holds, when known",
                      "type": [
                        "null",
                        "number"
                      ]
                    },
                    "private": {
                      "description": "true for an item private to this workspace, false for a global catalogue item",
                      "type": "boolean"
                    },
                    "series": {
                      "description": "series or variety",
                      "type": "string"
                    },
                    "size": {
                      "description": "size, free text",
                      "type": "string"
                    },
                    "stock_qty": {
                      "description": "quantity this workspace holds, in unit",
                      "type": "number"
                    },
                    "unit": {
                      "description": "unit the quantity is counted in",
                      "type": "string"
                    }
                  },
                  "required": [
                    "id",
                    "private",
                    "category",
                    "category_name",
                    "name",
                    "unit",
                    "stock_qty",
                    "in_inventory"
                  ],
                  "type": "object"
                },
                "qty_planned": {
                  "description": "planned quantity, absent when no plan is set",
                  "type": [
                    "null",
                    "number"
                  ]
                },
                "qty_used": {
                  "description": "quantity already used by the project",
                  "type": "number"
                },
                "shortage": {
                  "description": "max(0, qty_planned - qty_used - stock): how much is still to buy",
                  "type": "number"
                }
              },
              "required": [
                "item",
                "qty_used",
                "shortage"
              ],
              "type": "object"
            },
            "type": [
              "null",
              "array"
            ]
          },
          "name": {
            "description": "project name",
            "type": "string"
          },
          "notes": {
            "description": "free-text notes",
            "type": "string"
          }
        },
        "required": [
          "id",
          "name",
          "material_count",
          "materials",
          "created_at"
        ],
        "type": "object"
      },
      "type": [
        "null",
        "array"
      ]
    }
  },
  "required": [
    "projects"
  ],
  "type": "object"
}
```

### `craft_project_set_material`

Plan a material for a project

Add a catalogue item to the project plan or change its planned quantity. Identify the item by item_id or by manufacturer + article. A missing qty_planned clears the plan; used quantity and stock are untouched.

**Доступ:** изменяет. Повторный вызов не создаёт нового состояния; ответ может отличаться (например, «уже подтверждено»).

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "article": {
      "description": "article number, with manufacturer, instead of item_id",
      "type": "string"
    },
    "category": {
      "description": "category slug, only to disambiguate manufacturer + article",
      "type": "string"
    },
    "item_id": {
      "description": "catalogue item id (UUID); use it when known",
      "type": "string"
    },
    "manufacturer": {
      "description": "manufacturer name, with article, instead of item_id",
      "type": "string"
    },
    "project_id": {
      "description": "project id (UUID)",
      "type": "string"
    },
    "qty_planned": {
      "description": "planned quantity, 0 or more; omit to clear the plan",
      "type": [
        "null",
        "number"
      ]
    },
    "space": {
      "description": "warehouse space id or name (see spaces_list); may be omitted when the user has exactly one",
      "type": "string"
    }
  },
  "required": [
    "project_id"
  ],
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "created_at": {
      "description": "creation time in RFC 3339 UTC",
      "type": "string"
    },
    "id": {
      "description": "project id (UUID)",
      "type": "string"
    },
    "material_count": {
      "description": "number of materials planned",
      "type": "integer"
    },
    "materials": {
      "description": "materials; empty when the project is fresh or listed",
      "items": {
        "additionalProperties": false,
        "properties": {
          "item": {
            "additionalProperties": false,
            "description": "the catalogue item with the current stock",
            "properties": {
              "article": {
                "description": "manufacturer article number",
                "type": "string"
              },
              "attrs": {
                "additionalProperties": true,
                "description": "category specific attributes",
                "type": "object"
              },
              "category": {
                "description": "category slug",
                "type": "string"
              },
              "category_name": {
                "description": "category name in Russian",
                "type": "string"
              },
              "color_hex": {
                "description": "colour as #RRGGBB",
                "type": "string"
              },
              "color_name": {
                "description": "colour name",
                "type": "string"
              },
              "color_source": {
                "description": "how trustworthy color_hex is: official, approx, or photo (computed from the product photo, rough)",
                "type": "string"
              },
              "finish": {
                "description": "finish or coating, free text",
                "type": "string"
              },
              "id": {
                "description": "catalogue item id (UUID)",
                "type": "string"
              },
              "image_url": {
                "description": "link to a photo of the item",
                "type": "string"
              },
              "in_inventory": {
                "description": "whether the workspace has a stock row for the item",
                "type": "boolean"
              },
              "manufacturer": {
                "description": "manufacturer name",
                "type": "string"
              },
              "name": {
                "description": "item name",
                "type": "string"
              },
              "pack_size": {
                "description": "how many base units one pack holds, when known",
                "type": [
                  "null",
                  "number"
                ]
              },
              "private": {
                "description": "true for an item private to this workspace, false for a global catalogue item",
                "type": "boolean"
              },
              "series": {
                "description": "series or variety",
                "type": "string"
              },
              "size": {
                "description": "size, free text",
                "type": "string"
              },
              "stock_qty": {
                "description": "quantity this workspace holds, in unit",
                "type": "number"
              },
              "unit": {
                "description": "unit the quantity is counted in",
                "type": "string"
              }
            },
            "required": [
              "id",
              "private",
              "category",
              "category_name",
              "name",
              "unit",
              "stock_qty",
              "in_inventory"
            ],
            "type": "object"
          },
          "qty_planned": {
            "description": "planned quantity, absent when no plan is set",
            "type": [
              "null",
              "number"
            ]
          },
          "qty_used": {
            "description": "quantity already used by the project",
            "type": "number"
          },
          "shortage": {
            "description": "max(0, qty_planned - qty_used - stock): how much is still to buy",
            "type": "number"
          }
        },
        "required": [
          "item",
          "qty_used",
          "shortage"
        ],
        "type": "object"
      },
      "type": [
        "null",
        "array"
      ]
    },
    "name": {
      "description": "project name",
      "type": "string"
    },
    "notes": {
      "description": "free-text notes",
      "type": "string"
    }
  },
  "required": [
    "id",
    "name",
    "material_count",
    "materials",
    "created_at"
  ],
  "type": "object"
}
```

### `craft_project_shortage_to_wishlist`

Add the project shortage to the wish list

Add every material of the project whose shortage is positive (the quantity still to buy) to the wish list, skipping materials that are already wanted. Returns the added wishes; a repeated call adds nothing.

**Доступ:** изменяет. Повторный вызов не создаёт нового состояния; ответ может отличаться (например, «уже подтверждено»).

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "project_id": {
      "description": "project id (UUID)",
      "type": "string"
    },
    "space": {
      "description": "warehouse space id or name (see spaces_list); may be omitted when the user has exactly one",
      "type": "string"
    }
  },
  "required": [
    "project_id"
  ],
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "wishes": {
      "description": "wish list entries; empty when there are none",
      "items": {
        "additionalProperties": false,
        "properties": {
          "catalog_item_id": {
            "description": "catalogue item id, absent for a free-text wish",
            "type": "string"
          },
          "created_at": {
            "description": "creation time in RFC 3339 UTC",
            "type": "string"
          },
          "id": {
            "description": "wish list entry id (UUID)",
            "type": "string"
          },
          "item": {
            "additionalProperties": false,
            "description": "catalogue item summary with the current stock, absent for a free-text wish",
            "properties": {
              "article": {
                "description": "manufacturer article number",
                "type": "string"
              },
              "attrs": {
                "additionalProperties": true,
                "description": "category specific attributes",
                "type": "object"
              },
              "category": {
                "description": "category slug",
                "type": "string"
              },
              "category_name": {
                "description": "category name in Russian",
                "type": "string"
              },
              "color_hex": {
                "description": "colour as #RRGGBB",
                "type": "string"
              },
              "color_name": {
                "description": "colour name",
                "type": "string"
              },
              "color_source": {
                "description": "how trustworthy color_hex is: official, approx, or photo (computed from the product photo, rough)",
                "type": "string"
              },
              "finish": {
                "description": "finish or coating, free text",
                "type": "string"
              },
              "id": {
                "description": "catalogue item id (UUID)",
                "type": "string"
              },
              "image_url": {
                "description": "link to a photo of the item",
                "type": "string"
              },
              "in_inventory": {
                "description": "whether the workspace has a stock row for the item",
                "type": "boolean"
              },
              "manufacturer": {
                "description": "manufacturer name",
                "type": "string"
              },
              "name": {
                "description": "item name",
                "type": "string"
              },
              "pack_size": {
                "description": "how many base units one pack holds, when known",
                "type": [
                  "null",
                  "number"
                ]
              },
              "private": {
                "description": "true for an item private to this workspace, false for a global catalogue item",
                "type": "boolean"
              },
              "series": {
                "description": "series or variety",
                "type": "string"
              },
              "size": {
                "description": "size, free text",
                "type": "string"
              },
              "stock_qty": {
                "description": "quantity this workspace holds, in unit",
                "type": "number"
              },
              "unit": {
                "description": "unit the quantity is counted in",
                "type": "string"
              }
            },
            "required": [
              "id",
              "private",
              "category",
              "category_name",
              "name",
              "unit",
              "stock_qty",
              "in_inventory"
            ],
            "type": [
              "null",
              "object"
            ]
          },
          "links": {
            "description": "shop pages for this wish",
            "items": {
              "additionalProperties": false,
              "properties": {
                "created_at": {
                  "description": "creation time in RFC 3339 UTC",
                  "type": "string"
                },
                "currency": {
                  "description": "currency of price",
                  "type": "string"
                },
                "id": {
                  "description": "link id (UUID)",
                  "type": "string"
                },
                "in_stock": {
                  "description": "whether the shop had it in stock at the last check",
                  "type": [
                    "null",
                    "boolean"
                  ]
                },
                "price": {
                  "description": "price when known",
                  "type": [
                    "null",
                    "number"
                  ]
                },
                "shop_name": {
                  "description": "shop name",
                  "type": "string"
                },
                "url": {
                  "description": "shop page, http or https",
                  "type": "string"
                }
              },
              "required": [
                "id",
                "url",
                "created_at"
              ],
              "type": "object"
            },
            "type": [
              "null",
              "array"
            ]
          },
          "note": {
            "description": "free-text note",
            "type": "string"
          },
          "priority": {
            "description": "0 normal, 1 or 2 high",
            "type": "integer"
          },
          "qty_wanted": {
            "description": "how much is wanted, in the item unit",
            "type": [
              "null",
              "number"
            ]
          },
          "status": {
            "description": "wanted, ordered or bought",
            "type": "string"
          },
          "title": {
            "description": "what is wanted",
            "type": "string"
          },
          "updated_at": {
            "description": "last change time in RFC 3339 UTC",
            "type": "string"
          }
        },
        "required": [
          "id",
          "title",
          "priority",
          "status",
          "links",
          "created_at",
          "updated_at"
        ],
        "type": "object"
      },
      "type": [
        "null",
        "array"
      ]
    }
  },
  "required": [
    "wishes"
  ],
  "type": "object"
}
```

### `craft_project_use_material`

Use material for a project

Withdraw material from the stock for a project: decreases the stock and increases the used quantity of the material in the same transaction. A withdrawal larger than the stock is an error and nothing changes. The inventory log records the project name as the reason.

**Доступ:** изменяет. Каждый вызов создаёт новое состояние.

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "article": {
      "description": "article number, with manufacturer, instead of item_id",
      "type": "string"
    },
    "category": {
      "description": "category slug, only to disambiguate manufacturer + article",
      "type": "string"
    },
    "item_id": {
      "description": "catalogue item id (UUID); use it when known",
      "type": "string"
    },
    "manufacturer": {
      "description": "manufacturer name, with article, instead of item_id",
      "type": "string"
    },
    "project_id": {
      "description": "project id (UUID)",
      "type": "string"
    },
    "qty": {
      "description": "quantity to withdraw from the stock and mark used, positive",
      "type": "number"
    },
    "space": {
      "description": "warehouse space id or name (see spaces_list); may be omitted when the user has exactly one",
      "type": "string"
    }
  },
  "required": [
    "project_id",
    "qty"
  ],
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "created_at": {
      "description": "creation time in RFC 3339 UTC",
      "type": "string"
    },
    "id": {
      "description": "project id (UUID)",
      "type": "string"
    },
    "material_count": {
      "description": "number of materials planned",
      "type": "integer"
    },
    "materials": {
      "description": "materials; empty when the project is fresh or listed",
      "items": {
        "additionalProperties": false,
        "properties": {
          "item": {
            "additionalProperties": false,
            "description": "the catalogue item with the current stock",
            "properties": {
              "article": {
                "description": "manufacturer article number",
                "type": "string"
              },
              "attrs": {
                "additionalProperties": true,
                "description": "category specific attributes",
                "type": "object"
              },
              "category": {
                "description": "category slug",
                "type": "string"
              },
              "category_name": {
                "description": "category name in Russian",
                "type": "string"
              },
              "color_hex": {
                "description": "colour as #RRGGBB",
                "type": "string"
              },
              "color_name": {
                "description": "colour name",
                "type": "string"
              },
              "color_source": {
                "description": "how trustworthy color_hex is: official, approx, or photo (computed from the product photo, rough)",
                "type": "string"
              },
              "finish": {
                "description": "finish or coating, free text",
                "type": "string"
              },
              "id": {
                "description": "catalogue item id (UUID)",
                "type": "string"
              },
              "image_url": {
                "description": "link to a photo of the item",
                "type": "string"
              },
              "in_inventory": {
                "description": "whether the workspace has a stock row for the item",
                "type": "boolean"
              },
              "manufacturer": {
                "description": "manufacturer name",
                "type": "string"
              },
              "name": {
                "description": "item name",
                "type": "string"
              },
              "pack_size": {
                "description": "how many base units one pack holds, when known",
                "type": [
                  "null",
                  "number"
                ]
              },
              "private": {
                "description": "true for an item private to this workspace, false for a global catalogue item",
                "type": "boolean"
              },
              "series": {
                "description": "series or variety",
                "type": "string"
              },
              "size": {
                "description": "size, free text",
                "type": "string"
              },
              "stock_qty": {
                "description": "quantity this workspace holds, in unit",
                "type": "number"
              },
              "unit": {
                "description": "unit the quantity is counted in",
                "type": "string"
              }
            },
            "required": [
              "id",
              "private",
              "category",
              "category_name",
              "name",
              "unit",
              "stock_qty",
              "in_inventory"
            ],
            "type": "object"
          },
          "qty_planned": {
            "description": "planned quantity, absent when no plan is set",
            "type": [
              "null",
              "number"
            ]
          },
          "qty_used": {
            "description": "quantity already used by the project",
            "type": "number"
          },
          "shortage": {
            "description": "max(0, qty_planned - qty_used - stock): how much is still to buy",
            "type": "number"
          }
        },
        "required": [
          "item",
          "qty_used",
          "shortage"
        ],
        "type": "object"
      },
      "type": [
        "null",
        "array"
      ]
    },
    "name": {
      "description": "project name",
      "type": "string"
    },
    "notes": {
      "description": "free-text notes",
      "type": "string"
    }
  },
  "required": [
    "id",
    "name",
    "material_count",
    "materials",
    "created_at"
  ],
  "type": "object"
}
```

### `craft_reference`

Warehouse reference data

List how a warehouse is configured: material categories (with their default unit and extra fields), manufacturers, units and the official catalogues. Call it before searching or creating items to learn the valid categories, units and fields.

**Доступ:** только чтение. Инструмент ничего не меняет, его можно вызывать свободно.

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "space": {
      "description": "warehouse space id or name (see spaces_list); may be omitted when the user has exactly one",
      "type": "string"
    }
  },
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "catalogs": {
      "description": "official catalogues",
      "items": {
        "additionalProperties": false,
        "properties": {
          "enabled": {
            "description": "connected to this warehouse",
            "type": "boolean"
          },
          "items": {
            "description": "number of positions",
            "type": "integer"
          },
          "name": {
            "description": "catalogue name",
            "type": "string"
          },
          "source": {
            "description": "catalogue key for craft_catalogs_set",
            "type": "string"
          }
        },
        "required": [
          "source",
          "name",
          "enabled",
          "items"
        ],
        "type": "object"
      },
      "type": [
        "null",
        "array"
      ]
    },
    "categories": {
      "description": "categories of the warehouse",
      "items": {
        "additionalProperties": false,
        "properties": {
          "default_unit": {
            "description": "unit used when an item does not name one",
            "type": "string"
          },
          "fields": {
            "description": "extra fields; their values go to item attrs under key",
            "items": {
              "additionalProperties": false,
              "properties": {
                "key": {
                  "description": "name of the value in item attrs; omit for a new field",
                  "type": "string"
                },
                "label": {
                  "description": "label shown to people",
                  "type": "string"
                },
                "options": {
                  "description": "allowed values of a select field",
                  "items": {
                    "type": "string"
                  },
                  "type": [
                    "null",
                    "array"
                  ]
                },
                "type": {
                  "description": "text, number, bool or select",
                  "type": "string"
                }
              },
              "required": [
                "label",
                "type"
              ],
              "type": "object"
            },
            "type": [
              "null",
              "array"
            ]
          },
          "id": {
            "description": "category id",
            "type": "string"
          },
          "name": {
            "description": "category name",
            "type": "string"
          },
          "own_items": {
            "description": "how many of the warehouse's own items use it",
            "type": "integer"
          },
          "slug": {
            "description": "category slug to use in filters and item data (a name works too)",
            "type": "string"
          }
        },
        "required": [
          "id",
          "slug",
          "name",
          "default_unit",
          "fields",
          "own_items"
        ],
        "type": "object"
      },
      "type": [
        "null",
        "array"
      ]
    },
    "manufacturers": {
      "description": "own and connected official manufacturers",
      "items": {
        "additionalProperties": false,
        "properties": {
          "id": {
            "description": "manufacturer id",
            "type": "string"
          },
          "name": {
            "description": "manufacturer name",
            "type": "string"
          },
          "official": {
            "description": "from an official catalogue, read-only",
            "type": "boolean"
          },
          "website": {
            "description": "website",
            "type": "string"
          }
        },
        "required": [
          "id",
          "name",
          "official"
        ],
        "type": "object"
      },
      "type": [
        "null",
        "array"
      ]
    },
    "units": {
      "description": "units of the warehouse",
      "items": {
        "additionalProperties": false,
        "properties": {
          "id": {
            "description": "unit id",
            "type": "string"
          },
          "name": {
            "description": "unit name",
            "type": "string"
          }
        },
        "required": [
          "id",
          "name"
        ],
        "type": "object"
      },
      "type": [
        "null",
        "array"
      ]
    }
  },
  "required": [
    "categories",
    "manufacturers",
    "units",
    "catalogs"
  ],
  "type": "object"
}
```

### `craft_stock_adjust`

Change stock by a delta

Add to or use up material: delta is positive to add and negative to use. Identify the item by item_id, or by manufacturer + article (+ category when ambiguous). The stock row is created on first use. A withdrawal larger than the stock is an error. The change is logged with the optional reason.

**Доступ:** изменяет. Каждый вызов создаёт новое состояние.

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "article": {
      "description": "article number, with manufacturer, instead of item_id",
      "type": "string"
    },
    "category": {
      "description": "category slug, only to disambiguate manufacturer + article",
      "type": "string"
    },
    "delta": {
      "description": "change of the quantity in the item's unit: positive adds, negative uses up",
      "type": "number"
    },
    "item_id": {
      "description": "catalogue item id (UUID); use it when known",
      "type": "string"
    },
    "manufacturer": {
      "description": "manufacturer name, with article, instead of item_id",
      "type": "string"
    },
    "reason": {
      "description": "why, for example the project name",
      "type": "string"
    },
    "space": {
      "description": "warehouse space id or name (see spaces_list); may be omitted when the user has exactly one",
      "type": "string"
    }
  },
  "required": [
    "delta"
  ],
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "item": {
      "additionalProperties": false,
      "description": "the catalogue item",
      "properties": {
        "article": {
          "description": "manufacturer article number",
          "type": "string"
        },
        "attrs": {
          "additionalProperties": true,
          "description": "category specific attributes",
          "type": "object"
        },
        "category": {
          "description": "category slug",
          "type": "string"
        },
        "category_name": {
          "description": "category name in Russian",
          "type": "string"
        },
        "color_hex": {
          "description": "colour as #RRGGBB",
          "type": "string"
        },
        "color_name": {
          "description": "colour name",
          "type": "string"
        },
        "color_source": {
          "description": "how trustworthy color_hex is: official, approx, or photo (computed from the product photo, rough)",
          "type": "string"
        },
        "finish": {
          "description": "finish or coating, free text",
          "type": "string"
        },
        "id": {
          "description": "catalogue item id (UUID)",
          "type": "string"
        },
        "image_url": {
          "description": "link to a photo of the item",
          "type": "string"
        },
        "in_inventory": {
          "description": "whether the workspace has a stock row for the item",
          "type": "boolean"
        },
        "manufacturer": {
          "description": "manufacturer name",
          "type": "string"
        },
        "name": {
          "description": "item name",
          "type": "string"
        },
        "pack_size": {
          "description": "how many base units one pack holds, when known",
          "type": [
            "null",
            "number"
          ]
        },
        "private": {
          "description": "true for an item private to this workspace, false for a global catalogue item",
          "type": "boolean"
        },
        "series": {
          "description": "series or variety",
          "type": "string"
        },
        "size": {
          "description": "size, free text",
          "type": "string"
        },
        "stock_qty": {
          "description": "quantity this workspace holds, in unit",
          "type": "number"
        },
        "unit": {
          "description": "unit the quantity is counted in",
          "type": "string"
        }
      },
      "required": [
        "id",
        "private",
        "category",
        "category_name",
        "name",
        "unit",
        "stock_qty",
        "in_inventory"
      ],
      "type": "object"
    },
    "location": {
      "description": "where it is stored",
      "type": "string"
    },
    "notes": {
      "description": "notes about this stock",
      "type": "string"
    },
    "qty": {
      "description": "quantity held, in item.unit",
      "type": "number"
    },
    "updated_at": {
      "description": "last change in RFC 3339 UTC",
      "type": "string"
    }
  },
  "required": [
    "item",
    "qty",
    "updated_at"
  ],
  "type": "object"
}
```

### `craft_stock_remove`

Remove an item from stock

Delete the stock row of an item (the catalogue item itself stays).

**Доступ:** изменяет и удаляет. Вызов может удалить данные; клиент должен подтверждать его у пользователя.

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "article": {
      "description": "article number, with manufacturer, instead of item_id",
      "type": "string"
    },
    "category": {
      "description": "category slug, only to disambiguate manufacturer + article",
      "type": "string"
    },
    "item_id": {
      "description": "catalogue item id (UUID); use it when known",
      "type": "string"
    },
    "manufacturer": {
      "description": "manufacturer name, with article, instead of item_id",
      "type": "string"
    },
    "space": {
      "description": "warehouse space id or name (see spaces_list); may be omitted when the user has exactly one",
      "type": "string"
    }
  },
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "item_id": {
      "description": "item whose stock row was removed",
      "type": "string"
    },
    "removed": {
      "description": "always true on success",
      "type": "boolean"
    }
  },
  "required": [
    "item_id",
    "removed"
  ],
  "type": "object"
}
```

### `craft_stock_set`

Set stock to a value

Set the quantity of an item to an absolute value (a stocktake). Identify the item like craft_stock_adjust. The change is logged.

**Доступ:** изменяет. Повторный вызов не создаёт нового состояния; ответ может отличаться (например, «уже подтверждено»).

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "article": {
      "description": "article number, with manufacturer, instead of item_id",
      "type": "string"
    },
    "category": {
      "description": "category slug, only to disambiguate manufacturer + article",
      "type": "string"
    },
    "item_id": {
      "description": "catalogue item id (UUID); use it when known",
      "type": "string"
    },
    "manufacturer": {
      "description": "manufacturer name, with article, instead of item_id",
      "type": "string"
    },
    "qty": {
      "description": "new absolute quantity in the item's unit, 0 or more",
      "type": "number"
    },
    "reason": {
      "description": "why, for example stocktake",
      "type": "string"
    },
    "space": {
      "description": "warehouse space id or name (see spaces_list); may be omitted when the user has exactly one",
      "type": "string"
    }
  },
  "required": [
    "qty"
  ],
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "item": {
      "additionalProperties": false,
      "description": "the catalogue item",
      "properties": {
        "article": {
          "description": "manufacturer article number",
          "type": "string"
        },
        "attrs": {
          "additionalProperties": true,
          "description": "category specific attributes",
          "type": "object"
        },
        "category": {
          "description": "category slug",
          "type": "string"
        },
        "category_name": {
          "description": "category name in Russian",
          "type": "string"
        },
        "color_hex": {
          "description": "colour as #RRGGBB",
          "type": "string"
        },
        "color_name": {
          "description": "colour name",
          "type": "string"
        },
        "color_source": {
          "description": "how trustworthy color_hex is: official, approx, or photo (computed from the product photo, rough)",
          "type": "string"
        },
        "finish": {
          "description": "finish or coating, free text",
          "type": "string"
        },
        "id": {
          "description": "catalogue item id (UUID)",
          "type": "string"
        },
        "image_url": {
          "description": "link to a photo of the item",
          "type": "string"
        },
        "in_inventory": {
          "description": "whether the workspace has a stock row for the item",
          "type": "boolean"
        },
        "manufacturer": {
          "description": "manufacturer name",
          "type": "string"
        },
        "name": {
          "description": "item name",
          "type": "string"
        },
        "pack_size": {
          "description": "how many base units one pack holds, when known",
          "type": [
            "null",
            "number"
          ]
        },
        "private": {
          "description": "true for an item private to this workspace, false for a global catalogue item",
          "type": "boolean"
        },
        "series": {
          "description": "series or variety",
          "type": "string"
        },
        "size": {
          "description": "size, free text",
          "type": "string"
        },
        "stock_qty": {
          "description": "quantity this workspace holds, in unit",
          "type": "number"
        },
        "unit": {
          "description": "unit the quantity is counted in",
          "type": "string"
        }
      },
      "required": [
        "id",
        "private",
        "category",
        "category_name",
        "name",
        "unit",
        "stock_qty",
        "in_inventory"
      ],
      "type": "object"
    },
    "location": {
      "description": "where it is stored",
      "type": "string"
    },
    "notes": {
      "description": "notes about this stock",
      "type": "string"
    },
    "qty": {
      "description": "quantity held, in item.unit",
      "type": "number"
    },
    "updated_at": {
      "description": "last change in RFC 3339 UTC",
      "type": "string"
    }
  },
  "required": [
    "item",
    "qty",
    "updated_at"
  ],
  "type": "object"
}
```

### `craft_unit_delete`

Delete a unit

Delete a unit that no own item and no category uses.

**Доступ:** изменяет и удаляет. Вызов может удалить данные; клиент должен подтверждать его у пользователя.

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "id": {
      "description": "id from craft_reference",
      "type": "string"
    },
    "space": {
      "description": "warehouse space id or name (see spaces_list); may be omitted when the user has exactly one",
      "type": "string"
    }
  },
  "required": [
    "id"
  ],
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "deleted": {
      "description": "always true on success",
      "type": "boolean"
    },
    "id": {
      "description": "deleted id",
      "type": "string"
    }
  },
  "required": [
    "id",
    "deleted"
  ],
  "type": "object"
}
```

### `craft_unit_save`

Create or rename a unit

Create a unit (omit id) or rename one. A rename follows into own items and categories counted in it; quantities are not converted.

**Доступ:** изменяет. Каждый вызов создаёт новое состояние.

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "id": {
      "description": "unit to rename; omit to create",
      "type": "string"
    },
    "name": {
      "description": "unit name, up to 24 characters",
      "type": "string"
    },
    "space": {
      "description": "warehouse space id or name (see spaces_list); may be omitted when the user has exactly one",
      "type": "string"
    }
  },
  "required": [
    "name"
  ],
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "id": {
      "description": "unit id",
      "type": "string"
    },
    "name": {
      "description": "unit name",
      "type": "string"
    }
  },
  "required": [
    "id",
    "name"
  ],
  "type": "object"
}
```

### `craft_wishlist_add`

Add a wish

Add an entry to the wish list: either a catalogue item (catalog_item_id) or free text (title) for something that is not in any catalogue yet. The title defaults to the item name. qty_wanted is optional and must be positive; priority is 0 (normal), 1 or 2 (high). A catalogue item can be wanted only once at a time.

**Доступ:** изменяет. Каждый вызов создаёт новое состояние.

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "catalog_item_id": {
      "description": "catalogue item id (UUID); omit for a free-text wish",
      "type": "string"
    },
    "note": {
      "description": "free-text note",
      "type": "string"
    },
    "priority": {
      "description": "0 normal, 1 or 2 high",
      "type": "integer"
    },
    "qty_wanted": {
      "description": "how much is wanted, positive, in the item unit",
      "type": [
        "null",
        "number"
      ]
    },
    "space": {
      "description": "warehouse space id or name (see spaces_list); may be omitted when the user has exactly one",
      "type": "string"
    },
    "title": {
      "description": "what is wanted; defaults to the item name when catalog_item_id is set",
      "type": "string"
    }
  },
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "catalog_item_id": {
      "description": "catalogue item id, absent for a free-text wish",
      "type": "string"
    },
    "created_at": {
      "description": "creation time in RFC 3339 UTC",
      "type": "string"
    },
    "id": {
      "description": "wish list entry id (UUID)",
      "type": "string"
    },
    "item": {
      "additionalProperties": false,
      "description": "catalogue item summary with the current stock, absent for a free-text wish",
      "properties": {
        "article": {
          "description": "manufacturer article number",
          "type": "string"
        },
        "attrs": {
          "additionalProperties": true,
          "description": "category specific attributes",
          "type": "object"
        },
        "category": {
          "description": "category slug",
          "type": "string"
        },
        "category_name": {
          "description": "category name in Russian",
          "type": "string"
        },
        "color_hex": {
          "description": "colour as #RRGGBB",
          "type": "string"
        },
        "color_name": {
          "description": "colour name",
          "type": "string"
        },
        "color_source": {
          "description": "how trustworthy color_hex is: official, approx, or photo (computed from the product photo, rough)",
          "type": "string"
        },
        "finish": {
          "description": "finish or coating, free text",
          "type": "string"
        },
        "id": {
          "description": "catalogue item id (UUID)",
          "type": "string"
        },
        "image_url": {
          "description": "link to a photo of the item",
          "type": "string"
        },
        "in_inventory": {
          "description": "whether the workspace has a stock row for the item",
          "type": "boolean"
        },
        "manufacturer": {
          "description": "manufacturer name",
          "type": "string"
        },
        "name": {
          "description": "item name",
          "type": "string"
        },
        "pack_size": {
          "description": "how many base units one pack holds, when known",
          "type": [
            "null",
            "number"
          ]
        },
        "private": {
          "description": "true for an item private to this workspace, false for a global catalogue item",
          "type": "boolean"
        },
        "series": {
          "description": "series or variety",
          "type": "string"
        },
        "size": {
          "description": "size, free text",
          "type": "string"
        },
        "stock_qty": {
          "description": "quantity this workspace holds, in unit",
          "type": "number"
        },
        "unit": {
          "description": "unit the quantity is counted in",
          "type": "string"
        }
      },
      "required": [
        "id",
        "private",
        "category",
        "category_name",
        "name",
        "unit",
        "stock_qty",
        "in_inventory"
      ],
      "type": [
        "null",
        "object"
      ]
    },
    "links": {
      "description": "shop pages for this wish",
      "items": {
        "additionalProperties": false,
        "properties": {
          "created_at": {
            "description": "creation time in RFC 3339 UTC",
            "type": "string"
          },
          "currency": {
            "description": "currency of price",
            "type": "string"
          },
          "id": {
            "description": "link id (UUID)",
            "type": "string"
          },
          "in_stock": {
            "description": "whether the shop had it in stock at the last check",
            "type": [
              "null",
              "boolean"
            ]
          },
          "price": {
            "description": "price when known",
            "type": [
              "null",
              "number"
            ]
          },
          "shop_name": {
            "description": "shop name",
            "type": "string"
          },
          "url": {
            "description": "shop page, http or https",
            "type": "string"
          }
        },
        "required": [
          "id",
          "url",
          "created_at"
        ],
        "type": "object"
      },
      "type": [
        "null",
        "array"
      ]
    },
    "note": {
      "description": "free-text note",
      "type": "string"
    },
    "priority": {
      "description": "0 normal, 1 or 2 high",
      "type": "integer"
    },
    "qty_wanted": {
      "description": "how much is wanted, in the item unit",
      "type": [
        "null",
        "number"
      ]
    },
    "status": {
      "description": "wanted, ordered or bought",
      "type": "string"
    },
    "title": {
      "description": "what is wanted",
      "type": "string"
    },
    "updated_at": {
      "description": "last change time in RFC 3339 UTC",
      "type": "string"
    }
  },
  "required": [
    "id",
    "title",
    "priority",
    "status",
    "links",
    "created_at",
    "updated_at"
  ],
  "type": "object"
}
```

### `craft_wishlist_link_add`

Add a shop link to a wish

Attach a shop page where the wished item can be bought: an http(s) url, the shop name, and optionally the price, currency and whether the shop has it in stock.

**Доступ:** изменяет. Каждый вызов создаёт новое состояние.

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "currency": {
      "description": "currency of price, for example RUB or EUR",
      "type": "string"
    },
    "in_stock": {
      "description": "whether the shop has it in stock",
      "type": [
        "null",
        "boolean"
      ]
    },
    "price": {
      "description": "price, 0 or more",
      "type": [
        "null",
        "number"
      ]
    },
    "shop_name": {
      "description": "shop name",
      "type": "string"
    },
    "space": {
      "description": "warehouse space id or name (see spaces_list); may be omitted when the user has exactly one",
      "type": "string"
    },
    "url": {
      "description": "shop page, must start with http:// or https://",
      "type": "string"
    },
    "wish_id": {
      "description": "wish list entry id (UUID)",
      "type": "string"
    }
  },
  "required": [
    "wish_id",
    "url"
  ],
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "created_at": {
      "description": "creation time in RFC 3339 UTC",
      "type": "string"
    },
    "currency": {
      "description": "currency of price",
      "type": "string"
    },
    "id": {
      "description": "link id (UUID)",
      "type": "string"
    },
    "in_stock": {
      "description": "whether the shop had it in stock at the last check",
      "type": [
        "null",
        "boolean"
      ]
    },
    "price": {
      "description": "price when known",
      "type": [
        "null",
        "number"
      ]
    },
    "shop_name": {
      "description": "shop name",
      "type": "string"
    },
    "url": {
      "description": "shop page, http or https",
      "type": "string"
    }
  },
  "required": [
    "id",
    "url",
    "created_at"
  ],
  "type": "object"
}
```

### `craft_wishlist_link_remove`

Remove a shop link from a wish

Delete one shop link of a wish list entry.

**Доступ:** изменяет и удаляет. Вызов может удалить данные; клиент должен подтверждать его у пользователя.

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "link_id": {
      "description": "shop link id (UUID)",
      "type": "string"
    },
    "space": {
      "description": "warehouse space id or name (see spaces_list); may be omitted when the user has exactly one",
      "type": "string"
    },
    "wish_id": {
      "description": "wish list entry id (UUID)",
      "type": "string"
    }
  },
  "required": [
    "wish_id",
    "link_id"
  ],
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "link_id": {
      "description": "id of the removed shop link",
      "type": "string"
    },
    "removed": {
      "description": "always true on success",
      "type": "boolean"
    },
    "wish_id": {
      "description": "wish list entry the link belonged to",
      "type": "string"
    }
  },
  "required": [
    "wish_id",
    "link_id",
    "removed"
  ],
  "type": "object"
}
```

### `craft_wishlist_list`

List the wish list

List what the workspace wants to buy. By default only entries with status wanted are returned; pass ordered or bought for another status, or all for every entry. Each entry shows its catalogue item (with the current stock), its links to shops and qty_wanted.

**Доступ:** только чтение. Инструмент ничего не меняет, его можно вызывать свободно.

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "space": {
      "description": "warehouse space id or name (see spaces_list); may be omitted when the user has exactly one",
      "type": "string"
    },
    "status": {
      "description": "wanted (default), ordered, bought or all",
      "type": "string"
    }
  },
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "wishes": {
      "description": "wish list entries; empty when there are none",
      "items": {
        "additionalProperties": false,
        "properties": {
          "catalog_item_id": {
            "description": "catalogue item id, absent for a free-text wish",
            "type": "string"
          },
          "created_at": {
            "description": "creation time in RFC 3339 UTC",
            "type": "string"
          },
          "id": {
            "description": "wish list entry id (UUID)",
            "type": "string"
          },
          "item": {
            "additionalProperties": false,
            "description": "catalogue item summary with the current stock, absent for a free-text wish",
            "properties": {
              "article": {
                "description": "manufacturer article number",
                "type": "string"
              },
              "attrs": {
                "additionalProperties": true,
                "description": "category specific attributes",
                "type": "object"
              },
              "category": {
                "description": "category slug",
                "type": "string"
              },
              "category_name": {
                "description": "category name in Russian",
                "type": "string"
              },
              "color_hex": {
                "description": "colour as #RRGGBB",
                "type": "string"
              },
              "color_name": {
                "description": "colour name",
                "type": "string"
              },
              "color_source": {
                "description": "how trustworthy color_hex is: official, approx, or photo (computed from the product photo, rough)",
                "type": "string"
              },
              "finish": {
                "description": "finish or coating, free text",
                "type": "string"
              },
              "id": {
                "description": "catalogue item id (UUID)",
                "type": "string"
              },
              "image_url": {
                "description": "link to a photo of the item",
                "type": "string"
              },
              "in_inventory": {
                "description": "whether the workspace has a stock row for the item",
                "type": "boolean"
              },
              "manufacturer": {
                "description": "manufacturer name",
                "type": "string"
              },
              "name": {
                "description": "item name",
                "type": "string"
              },
              "pack_size": {
                "description": "how many base units one pack holds, when known",
                "type": [
                  "null",
                  "number"
                ]
              },
              "private": {
                "description": "true for an item private to this workspace, false for a global catalogue item",
                "type": "boolean"
              },
              "series": {
                "description": "series or variety",
                "type": "string"
              },
              "size": {
                "description": "size, free text",
                "type": "string"
              },
              "stock_qty": {
                "description": "quantity this workspace holds, in unit",
                "type": "number"
              },
              "unit": {
                "description": "unit the quantity is counted in",
                "type": "string"
              }
            },
            "required": [
              "id",
              "private",
              "category",
              "category_name",
              "name",
              "unit",
              "stock_qty",
              "in_inventory"
            ],
            "type": [
              "null",
              "object"
            ]
          },
          "links": {
            "description": "shop pages for this wish",
            "items": {
              "additionalProperties": false,
              "properties": {
                "created_at": {
                  "description": "creation time in RFC 3339 UTC",
                  "type": "string"
                },
                "currency": {
                  "description": "currency of price",
                  "type": "string"
                },
                "id": {
                  "description": "link id (UUID)",
                  "type": "string"
                },
                "in_stock": {
                  "description": "whether the shop had it in stock at the last check",
                  "type": [
                    "null",
                    "boolean"
                  ]
                },
                "price": {
                  "description": "price when known",
                  "type": [
                    "null",
                    "number"
                  ]
                },
                "shop_name": {
                  "description": "shop name",
                  "type": "string"
                },
                "url": {
                  "description": "shop page, http or https",
                  "type": "string"
                }
              },
              "required": [
                "id",
                "url",
                "created_at"
              ],
              "type": "object"
            },
            "type": [
              "null",
              "array"
            ]
          },
          "note": {
            "description": "free-text note",
            "type": "string"
          },
          "priority": {
            "description": "0 normal, 1 or 2 high",
            "type": "integer"
          },
          "qty_wanted": {
            "description": "how much is wanted, in the item unit",
            "type": [
              "null",
              "number"
            ]
          },
          "status": {
            "description": "wanted, ordered or bought",
            "type": "string"
          },
          "title": {
            "description": "what is wanted",
            "type": "string"
          },
          "updated_at": {
            "description": "last change time in RFC 3339 UTC",
            "type": "string"
          }
        },
        "required": [
          "id",
          "title",
          "priority",
          "status",
          "links",
          "created_at",
          "updated_at"
        ],
        "type": "object"
      },
      "type": [
        "null",
        "array"
      ]
    }
  },
  "required": [
    "wishes"
  ],
  "type": "object"
}
```

### `craft_wishlist_mark_bought`

Mark a wish as bought

Mark a wish list entry as bought. With add_to_stock set and a catalogue item, the wanted quantity is added to the stock in the same transaction, exactly like craft_stock_adjust; a free-text entry is only marked bought. Repeating the call does not add the quantity twice.

**Доступ:** изменяет. Повторный вызов не создаёт нового состояния; ответ может отличаться (например, «уже подтверждено»).

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "add_to_stock": {
      "description": "add the wanted quantity to the stock when the entry points at a catalogue item",
      "type": "boolean"
    },
    "space": {
      "description": "warehouse space id or name (see spaces_list); may be omitted when the user has exactly one",
      "type": "string"
    },
    "wish_id": {
      "description": "wish list entry id (UUID)",
      "type": "string"
    }
  },
  "required": [
    "wish_id"
  ],
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "catalog_item_id": {
      "description": "catalogue item id, absent for a free-text wish",
      "type": "string"
    },
    "created_at": {
      "description": "creation time in RFC 3339 UTC",
      "type": "string"
    },
    "id": {
      "description": "wish list entry id (UUID)",
      "type": "string"
    },
    "item": {
      "additionalProperties": false,
      "description": "catalogue item summary with the current stock, absent for a free-text wish",
      "properties": {
        "article": {
          "description": "manufacturer article number",
          "type": "string"
        },
        "attrs": {
          "additionalProperties": true,
          "description": "category specific attributes",
          "type": "object"
        },
        "category": {
          "description": "category slug",
          "type": "string"
        },
        "category_name": {
          "description": "category name in Russian",
          "type": "string"
        },
        "color_hex": {
          "description": "colour as #RRGGBB",
          "type": "string"
        },
        "color_name": {
          "description": "colour name",
          "type": "string"
        },
        "color_source": {
          "description": "how trustworthy color_hex is: official, approx, or photo (computed from the product photo, rough)",
          "type": "string"
        },
        "finish": {
          "description": "finish or coating, free text",
          "type": "string"
        },
        "id": {
          "description": "catalogue item id (UUID)",
          "type": "string"
        },
        "image_url": {
          "description": "link to a photo of the item",
          "type": "string"
        },
        "in_inventory": {
          "description": "whether the workspace has a stock row for the item",
          "type": "boolean"
        },
        "manufacturer": {
          "description": "manufacturer name",
          "type": "string"
        },
        "name": {
          "description": "item name",
          "type": "string"
        },
        "pack_size": {
          "description": "how many base units one pack holds, when known",
          "type": [
            "null",
            "number"
          ]
        },
        "private": {
          "description": "true for an item private to this workspace, false for a global catalogue item",
          "type": "boolean"
        },
        "series": {
          "description": "series or variety",
          "type": "string"
        },
        "size": {
          "description": "size, free text",
          "type": "string"
        },
        "stock_qty": {
          "description": "quantity this workspace holds, in unit",
          "type": "number"
        },
        "unit": {
          "description": "unit the quantity is counted in",
          "type": "string"
        }
      },
      "required": [
        "id",
        "private",
        "category",
        "category_name",
        "name",
        "unit",
        "stock_qty",
        "in_inventory"
      ],
      "type": [
        "null",
        "object"
      ]
    },
    "links": {
      "description": "shop pages for this wish",
      "items": {
        "additionalProperties": false,
        "properties": {
          "created_at": {
            "description": "creation time in RFC 3339 UTC",
            "type": "string"
          },
          "currency": {
            "description": "currency of price",
            "type": "string"
          },
          "id": {
            "description": "link id (UUID)",
            "type": "string"
          },
          "in_stock": {
            "description": "whether the shop had it in stock at the last check",
            "type": [
              "null",
              "boolean"
            ]
          },
          "price": {
            "description": "price when known",
            "type": [
              "null",
              "number"
            ]
          },
          "shop_name": {
            "description": "shop name",
            "type": "string"
          },
          "url": {
            "description": "shop page, http or https",
            "type": "string"
          }
        },
        "required": [
          "id",
          "url",
          "created_at"
        ],
        "type": "object"
      },
      "type": [
        "null",
        "array"
      ]
    },
    "note": {
      "description": "free-text note",
      "type": "string"
    },
    "priority": {
      "description": "0 normal, 1 or 2 high",
      "type": "integer"
    },
    "qty_wanted": {
      "description": "how much is wanted, in the item unit",
      "type": [
        "null",
        "number"
      ]
    },
    "status": {
      "description": "wanted, ordered or bought",
      "type": "string"
    },
    "title": {
      "description": "what is wanted",
      "type": "string"
    },
    "updated_at": {
      "description": "last change time in RFC 3339 UTC",
      "type": "string"
    }
  },
  "required": [
    "id",
    "title",
    "priority",
    "status",
    "links",
    "created_at",
    "updated_at"
  ],
  "type": "object"
}
```

### `craft_wishlist_remove`

Remove a wish

Delete a wish list entry together with its links. The stock is not changed.

**Доступ:** изменяет и удаляет. Вызов может удалить данные; клиент должен подтверждать его у пользователя.

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "space": {
      "description": "warehouse space id or name (see spaces_list); may be omitted when the user has exactly one",
      "type": "string"
    },
    "wish_id": {
      "description": "wish list entry id (UUID)",
      "type": "string"
    }
  },
  "required": [
    "wish_id"
  ],
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "removed": {
      "description": "always true on success",
      "type": "boolean"
    },
    "wish_id": {
      "description": "id of the removed wish list entry",
      "type": "string"
    }
  },
  "required": [
    "wish_id",
    "removed"
  ],
  "type": "object"
}
```

### `craft_wishlist_update`

Update a wish

Change the quantity, priority, note or status of a wish list entry. Omitted fields keep their current values; status is wanted, ordered or bought. Use craft_wishlist_mark_bought to also add the purchase to the stock.

**Доступ:** изменяет. Повторный вызов не создаёт нового состояния; ответ может отличаться (например, «уже подтверждено»).

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "note": {
      "description": "new note; omit to keep the current one",
      "type": [
        "null",
        "string"
      ]
    },
    "priority": {
      "description": "new priority 0..2; omit to keep the current one",
      "type": [
        "null",
        "integer"
      ]
    },
    "qty_wanted": {
      "description": "new wanted quantity, positive; omit to keep the current one",
      "type": [
        "null",
        "number"
      ]
    },
    "space": {
      "description": "warehouse space id or name (see spaces_list); may be omitted when the user has exactly one",
      "type": "string"
    },
    "status": {
      "description": "wanted, ordered or bought; omit to keep the current status",
      "type": "string"
    },
    "wish_id": {
      "description": "wish list entry id (UUID)",
      "type": "string"
    }
  },
  "required": [
    "wish_id"
  ],
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "catalog_item_id": {
      "description": "catalogue item id, absent for a free-text wish",
      "type": "string"
    },
    "created_at": {
      "description": "creation time in RFC 3339 UTC",
      "type": "string"
    },
    "id": {
      "description": "wish list entry id (UUID)",
      "type": "string"
    },
    "item": {
      "additionalProperties": false,
      "description": "catalogue item summary with the current stock, absent for a free-text wish",
      "properties": {
        "article": {
          "description": "manufacturer article number",
          "type": "string"
        },
        "attrs": {
          "additionalProperties": true,
          "description": "category specific attributes",
          "type": "object"
        },
        "category": {
          "description": "category slug",
          "type": "string"
        },
        "category_name": {
          "description": "category name in Russian",
          "type": "string"
        },
        "color_hex": {
          "description": "colour as #RRGGBB",
          "type": "string"
        },
        "color_name": {
          "description": "colour name",
          "type": "string"
        },
        "color_source": {
          "description": "how trustworthy color_hex is: official, approx, or photo (computed from the product photo, rough)",
          "type": "string"
        },
        "finish": {
          "description": "finish or coating, free text",
          "type": "string"
        },
        "id": {
          "description": "catalogue item id (UUID)",
          "type": "string"
        },
        "image_url": {
          "description": "link to a photo of the item",
          "type": "string"
        },
        "in_inventory": {
          "description": "whether the workspace has a stock row for the item",
          "type": "boolean"
        },
        "manufacturer": {
          "description": "manufacturer name",
          "type": "string"
        },
        "name": {
          "description": "item name",
          "type": "string"
        },
        "pack_size": {
          "description": "how many base units one pack holds, when known",
          "type": [
            "null",
            "number"
          ]
        },
        "private": {
          "description": "true for an item private to this workspace, false for a global catalogue item",
          "type": "boolean"
        },
        "series": {
          "description": "series or variety",
          "type": "string"
        },
        "size": {
          "description": "size, free text",
          "type": "string"
        },
        "stock_qty": {
          "description": "quantity this workspace holds, in unit",
          "type": "number"
        },
        "unit": {
          "description": "unit the quantity is counted in",
          "type": "string"
        }
      },
      "required": [
        "id",
        "private",
        "category",
        "category_name",
        "name",
        "unit",
        "stock_qty",
        "in_inventory"
      ],
      "type": [
        "null",
        "object"
      ]
    },
    "links": {
      "description": "shop pages for this wish",
      "items": {
        "additionalProperties": false,
        "properties": {
          "created_at": {
            "description": "creation time in RFC 3339 UTC",
            "type": "string"
          },
          "currency": {
            "description": "currency of price",
            "type": "string"
          },
          "id": {
            "description": "link id (UUID)",
            "type": "string"
          },
          "in_stock": {
            "description": "whether the shop had it in stock at the last check",
            "type": [
              "null",
              "boolean"
            ]
          },
          "price": {
            "description": "price when known",
            "type": [
              "null",
              "number"
            ]
          },
          "shop_name": {
            "description": "shop name",
            "type": "string"
          },
          "url": {
            "description": "shop page, http or https",
            "type": "string"
          }
        },
        "required": [
          "id",
          "url",
          "created_at"
        ],
        "type": "object"
      },
      "type": [
        "null",
        "array"
      ]
    },
    "note": {
      "description": "free-text note",
      "type": "string"
    },
    "priority": {
      "description": "0 normal, 1 or 2 high",
      "type": "integer"
    },
    "qty_wanted": {
      "description": "how much is wanted, in the item unit",
      "type": [
        "null",
        "number"
      ]
    },
    "status": {
      "description": "wanted, ordered or bought",
      "type": "string"
    },
    "title": {
      "description": "what is wanted",
      "type": "string"
    },
    "updated_at": {
      "description": "last change time in RFC 3339 UTC",
      "type": "string"
    }
  },
  "required": [
    "id",
    "title",
    "priority",
    "status",
    "links",
    "created_at",
    "updated_at"
  ],
  "type": "object"
}
```

### `reminders_complete`

Complete a reminder occurrence

Confirm the occurrence that is waiting for confirmation, exactly like the «✅ Выполнено» button in the chat. Repeats stop and the delivered chat message is updated. If the reminder has not fired, was already completed or was edited, the call is a tool error: there is nothing to confirm.

**Доступ:** изменяет. Повторный вызов не создаёт нового состояния; ответ может отличаться (например, «уже подтверждено»).

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "id": {
      "description": "reminder id (UUID) whose pending occurrence is confirmed",
      "type": "string"
    }
  },
  "required": [
    "id"
  ],
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "active_occurrence": {
      "additionalProperties": false,
      "description": "the firing that waits for confirmation, absent otherwise",
      "properties": {
        "completed_at": {
          "description": "confirmation time in RFC 3339 UTC",
          "type": "string"
        },
        "first_sent_at": {
          "description": "time of the first delivered message in RFC 3339 UTC",
          "type": "string"
        },
        "id": {
          "description": "occurrence id (UUID)",
          "type": "string"
        },
        "next_repeat_at": {
          "description": "next repeat time in RFC 3339 UTC, empty when there is none",
          "type": "string"
        },
        "repeat_count": {
          "description": "number of delivered messages, including the first one",
          "type": "integer"
        },
        "scheduled_at": {
          "description": "due time in RFC 3339 UTC",
          "type": "string"
        },
        "status": {
          "description": "active while waiting for confirmation, completed after confirmation, cancelled when the rule changed",
          "type": "string"
        }
      },
      "required": [
        "id",
        "status",
        "scheduled_at",
        "repeat_count"
      ],
      "type": [
        "null",
        "object"
      ]
    },
    "count": {
      "description": "total number of occurrences, zero when there is no count end",
      "type": "integer"
    },
    "created_at": {
      "description": "creation time in RFC 3339 UTC",
      "type": "string"
    },
    "enabled": {
      "description": "whether the reminder is scheduled",
      "type": "boolean"
    },
    "finished": {
      "description": "true when the recurrence has no future occurrence and the last one was confirmed",
      "type": "boolean"
    },
    "finished_at": {
      "description": "finish time in RFC 3339 UTC, empty while not finished",
      "type": "string"
    },
    "id": {
      "description": "reminder id (UUID)",
      "type": "string"
    },
    "interval": {
      "description": "every N days/weeks/months/years",
      "type": "integer"
    },
    "last_error": {
      "description": "last delivery error, empty when the reminder is delivered",
      "type": "string"
    },
    "message": {
      "description": "reminder text",
      "type": "string"
    },
    "month_days": {
      "description": "days of month, present when repeat is monthly and nth_weekday is absent",
      "items": {
        "type": "integer"
      },
      "type": [
        "null",
        "array"
      ]
    },
    "next_due_at": {
      "description": "next firing in RFC 3339 UTC, empty while the reminder is disabled or finished",
      "type": "string"
    },
    "nth_weekday": {
      "additionalProperties": false,
      "description": "nth weekday of the month, present when repeat is monthly and month_days is absent",
      "properties": {
        "position": {
          "description": "position of the weekday in the month: 1 = first, 2 = second, 3 = third, 4 = fourth, -1 = last",
          "type": "integer"
        },
        "weekday": {
          "description": "weekday name mon..sun or ISO number 1..7 (1 = Monday)",
          "type": "string"
        }
      },
      "required": [
        "position",
        "weekday"
      ],
      "type": [
        "null",
        "object"
      ]
    },
    "occurrences_fired": {
      "description": "how many occurrences already fired; editing resets it",
      "type": "integer"
    },
    "recipient_ids": {
      "description": "user ids who get the message",
      "items": {
        "type": "string"
      },
      "type": [
        "null",
        "array"
      ]
    },
    "repeat": {
      "description": "once, daily, weekly, monthly or yearly",
      "type": "string"
    },
    "repeat_minutes": {
      "description": "interval in minutes between repeats of an unconfirmed occurrence",
      "type": "integer"
    },
    "schedule_kind": {
      "description": "alias of repeat, kept for older clients",
      "type": "string"
    },
    "space_id": {
      "description": "reminders space the rule belongs to",
      "type": "string"
    },
    "start_date": {
      "description": "first possible day as YYYY-MM-DD in the reminder time zone",
      "type": "string"
    },
    "time": {
      "description": "local wall clock time HH:MM",
      "type": "string"
    },
    "time_zone": {
      "description": "IANA time zone of the schedule",
      "type": "string"
    },
    "until": {
      "description": "last allowed day as YYYY-MM-DD, empty when there is no date end",
      "type": "string"
    },
    "updated_at": {
      "description": "last change time in RFC 3339 UTC",
      "type": "string"
    },
    "weekdays": {
      "description": "weekly days as mon..sun, present when repeat is weekly",
      "items": {
        "type": "string"
      },
      "type": [
        "null",
        "array"
      ]
    }
  },
  "required": [
    "space_id",
    "recipient_ids",
    "id",
    "message",
    "repeat",
    "schedule_kind",
    "interval",
    "start_date",
    "occurrences_fired",
    "finished",
    "time",
    "time_zone",
    "repeat_minutes",
    "enabled",
    "created_at",
    "updated_at"
  ],
  "type": "object"
}
```

### `reminders_create`

Create a reminder

Create a reminder for the current user. The recurrence is chosen with repeat: once (needs date), daily, weekly (needs weekdays), monthly (month_days or nth_weekday such as the second Tuesday or the last Friday) or yearly. interval makes it every N days/weeks/months/years and is relative to start_date; until or count stops the repetition. The old shape without repeat still works: weekdays means weekly and month_days means monthly. Without a time zone the user's profile time zone is used. Returns the stored reminder with its next due time in UTC.

**Доступ:** изменяет. Каждый вызов создаёт новое состояние.

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "count": {
      "description": "total number of occurrences, 1..10000; the reminder stops after them. Do not combine with until",
      "type": "integer"
    },
    "date": {
      "description": "date of a one-time reminder as YYYY-MM-DD in the reminder time zone. Also accepted as start_date for repeat=once",
      "type": "string"
    },
    "enabled": {
      "description": "create the reminder already scheduled. Default true",
      "type": [
        "null",
        "boolean"
      ]
    },
    "interval": {
      "description": "every N days/weeks/months/years, 1..1000. Default 1, for example interval=2 with repeat=weekly means every second week",
      "type": "integer"
    },
    "message": {
      "description": "reminder text in the user's language, 1 to 1500 characters",
      "type": "string"
    },
    "month_days": {
      "description": "monthly day-of-month schedule: days 1..31. A day the month does not have means its last day. Use either weekdays, month_days or nth_weekday",
      "items": {
        "type": "integer"
      },
      "type": [
        "null",
        "array"
      ]
    },
    "nth_weekday": {
      "additionalProperties": false,
      "description": "monthly nth-weekday schedule, for example the second Tuesday. Use either weekdays, month_days or nth_weekday",
      "properties": {
        "position": {
          "description": "position of the weekday in the month: 1 = first, 2 = second, 3 = third, 4 = fourth, -1 = last",
          "type": "integer"
        },
        "weekday": {
          "description": "weekday name mon..sun or ISO number 1..7 (1 = Monday)",
          "type": "string"
        }
      },
      "required": [
        "position",
        "weekday"
      ],
      "type": [
        "null",
        "object"
      ]
    },
    "recipient_ids": {
      "description": "user ids of space members who get the message (see spaces_list). Default: only the current user",
      "items": {
        "type": "string"
      },
      "type": [
        "null",
        "array"
      ]
    },
    "repeat": {
      "description": "recurrence kind: once, daily, weekly, monthly or yearly. Omit it to use the old shape: weekdays means weekly, month_days means monthly",
      "type": "string"
    },
    "repeat_minutes": {
      "description": "how often an unconfirmed occurrence repeats, 1..1440 minutes. Default 10",
      "type": [
        "null",
        "integer"
      ]
    },
    "space": {
      "description": "reminders space id or name (see spaces_list). Default: the user's own personal space",
      "type": "string"
    },
    "start_date": {
      "description": "first possible day as YYYY-MM-DD in the reminder time zone; defaults to today. For repeat=yearly its month and day are the anniversary",
      "type": "string"
    },
    "time": {
      "description": "local wall clock time as HH:MM in 24-hour form, for example 09:30",
      "type": "string"
    },
    "timezone": {
      "description": "IANA time zone such as Europe/Moscow. Defaults to the user's profile time zone",
      "type": "string"
    },
    "until": {
      "description": "last allowed day as YYYY-MM-DD; the reminder stops after it. Do not combine with count",
      "type": "string"
    },
    "weekdays": {
      "description": "weekly schedule: day names mon,tue,wed,thu,fri,sat,sun or ISO numbers 1..7 (1 = Monday). Use either weekdays, month_days or nth_weekday",
      "items": {
        "type": "string"
      },
      "type": [
        "null",
        "array"
      ]
    }
  },
  "required": [
    "message",
    "time"
  ],
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "active_occurrence": {
      "additionalProperties": false,
      "description": "the firing that waits for confirmation, absent otherwise",
      "properties": {
        "completed_at": {
          "description": "confirmation time in RFC 3339 UTC",
          "type": "string"
        },
        "first_sent_at": {
          "description": "time of the first delivered message in RFC 3339 UTC",
          "type": "string"
        },
        "id": {
          "description": "occurrence id (UUID)",
          "type": "string"
        },
        "next_repeat_at": {
          "description": "next repeat time in RFC 3339 UTC, empty when there is none",
          "type": "string"
        },
        "repeat_count": {
          "description": "number of delivered messages, including the first one",
          "type": "integer"
        },
        "scheduled_at": {
          "description": "due time in RFC 3339 UTC",
          "type": "string"
        },
        "status": {
          "description": "active while waiting for confirmation, completed after confirmation, cancelled when the rule changed",
          "type": "string"
        }
      },
      "required": [
        "id",
        "status",
        "scheduled_at",
        "repeat_count"
      ],
      "type": [
        "null",
        "object"
      ]
    },
    "count": {
      "description": "total number of occurrences, zero when there is no count end",
      "type": "integer"
    },
    "created_at": {
      "description": "creation time in RFC 3339 UTC",
      "type": "string"
    },
    "enabled": {
      "description": "whether the reminder is scheduled",
      "type": "boolean"
    },
    "finished": {
      "description": "true when the recurrence has no future occurrence and the last one was confirmed",
      "type": "boolean"
    },
    "finished_at": {
      "description": "finish time in RFC 3339 UTC, empty while not finished",
      "type": "string"
    },
    "id": {
      "description": "reminder id (UUID)",
      "type": "string"
    },
    "interval": {
      "description": "every N days/weeks/months/years",
      "type": "integer"
    },
    "last_error": {
      "description": "last delivery error, empty when the reminder is delivered",
      "type": "string"
    },
    "message": {
      "description": "reminder text",
      "type": "string"
    },
    "month_days": {
      "description": "days of month, present when repeat is monthly and nth_weekday is absent",
      "items": {
        "type": "integer"
      },
      "type": [
        "null",
        "array"
      ]
    },
    "next_due_at": {
      "description": "next firing in RFC 3339 UTC, empty while the reminder is disabled or finished",
      "type": "string"
    },
    "nth_weekday": {
      "additionalProperties": false,
      "description": "nth weekday of the month, present when repeat is monthly and month_days is absent",
      "properties": {
        "position": {
          "description": "position of the weekday in the month: 1 = first, 2 = second, 3 = third, 4 = fourth, -1 = last",
          "type": "integer"
        },
        "weekday": {
          "description": "weekday name mon..sun or ISO number 1..7 (1 = Monday)",
          "type": "string"
        }
      },
      "required": [
        "position",
        "weekday"
      ],
      "type": [
        "null",
        "object"
      ]
    },
    "occurrences_fired": {
      "description": "how many occurrences already fired; editing resets it",
      "type": "integer"
    },
    "recipient_ids": {
      "description": "user ids who get the message",
      "items": {
        "type": "string"
      },
      "type": [
        "null",
        "array"
      ]
    },
    "repeat": {
      "description": "once, daily, weekly, monthly or yearly",
      "type": "string"
    },
    "repeat_minutes": {
      "description": "interval in minutes between repeats of an unconfirmed occurrence",
      "type": "integer"
    },
    "schedule_kind": {
      "description": "alias of repeat, kept for older clients",
      "type": "string"
    },
    "space_id": {
      "description": "reminders space the rule belongs to",
      "type": "string"
    },
    "start_date": {
      "description": "first possible day as YYYY-MM-DD in the reminder time zone",
      "type": "string"
    },
    "time": {
      "description": "local wall clock time HH:MM",
      "type": "string"
    },
    "time_zone": {
      "description": "IANA time zone of the schedule",
      "type": "string"
    },
    "until": {
      "description": "last allowed day as YYYY-MM-DD, empty when there is no date end",
      "type": "string"
    },
    "updated_at": {
      "description": "last change time in RFC 3339 UTC",
      "type": "string"
    },
    "weekdays": {
      "description": "weekly days as mon..sun, present when repeat is weekly",
      "items": {
        "type": "string"
      },
      "type": [
        "null",
        "array"
      ]
    }
  },
  "required": [
    "space_id",
    "recipient_ids",
    "id",
    "message",
    "repeat",
    "schedule_kind",
    "interval",
    "start_date",
    "occurrences_fired",
    "finished",
    "time",
    "time_zone",
    "repeat_minutes",
    "enabled",
    "created_at",
    "updated_at"
  ],
  "type": "object"
}
```

### `reminders_delete`

Delete a reminder

Delete a reminder and its occurrence history. Deleting an already deleted reminder is reported as not found.

**Доступ:** изменяет и удаляет. Вызов может удалить данные; клиент должен подтверждать его у пользователя.

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "id": {
      "description": "reminder id (UUID) to delete",
      "type": "string"
    }
  },
  "required": [
    "id"
  ],
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "deleted": {
      "description": "always true on success",
      "type": "boolean"
    },
    "id": {
      "description": "id of the deleted reminder",
      "type": "string"
    }
  },
  "required": [
    "id",
    "deleted"
  ],
  "type": "object"
}
```

### `reminders_get`

Get a reminder

Return one reminder by id together with its pending occurrence, if any. A reminder of another user is reported as not found.

**Доступ:** только чтение. Инструмент ничего не меняет, его можно вызывать свободно.

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "id": {
      "description": "reminder id (UUID) returned by reminders_list or reminders_create",
      "type": "string"
    }
  },
  "required": [
    "id"
  ],
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "active_occurrence": {
      "additionalProperties": false,
      "description": "the firing that waits for confirmation, absent otherwise",
      "properties": {
        "completed_at": {
          "description": "confirmation time in RFC 3339 UTC",
          "type": "string"
        },
        "first_sent_at": {
          "description": "time of the first delivered message in RFC 3339 UTC",
          "type": "string"
        },
        "id": {
          "description": "occurrence id (UUID)",
          "type": "string"
        },
        "next_repeat_at": {
          "description": "next repeat time in RFC 3339 UTC, empty when there is none",
          "type": "string"
        },
        "repeat_count": {
          "description": "number of delivered messages, including the first one",
          "type": "integer"
        },
        "scheduled_at": {
          "description": "due time in RFC 3339 UTC",
          "type": "string"
        },
        "status": {
          "description": "active while waiting for confirmation, completed after confirmation, cancelled when the rule changed",
          "type": "string"
        }
      },
      "required": [
        "id",
        "status",
        "scheduled_at",
        "repeat_count"
      ],
      "type": [
        "null",
        "object"
      ]
    },
    "count": {
      "description": "total number of occurrences, zero when there is no count end",
      "type": "integer"
    },
    "created_at": {
      "description": "creation time in RFC 3339 UTC",
      "type": "string"
    },
    "enabled": {
      "description": "whether the reminder is scheduled",
      "type": "boolean"
    },
    "finished": {
      "description": "true when the recurrence has no future occurrence and the last one was confirmed",
      "type": "boolean"
    },
    "finished_at": {
      "description": "finish time in RFC 3339 UTC, empty while not finished",
      "type": "string"
    },
    "id": {
      "description": "reminder id (UUID)",
      "type": "string"
    },
    "interval": {
      "description": "every N days/weeks/months/years",
      "type": "integer"
    },
    "last_error": {
      "description": "last delivery error, empty when the reminder is delivered",
      "type": "string"
    },
    "message": {
      "description": "reminder text",
      "type": "string"
    },
    "month_days": {
      "description": "days of month, present when repeat is monthly and nth_weekday is absent",
      "items": {
        "type": "integer"
      },
      "type": [
        "null",
        "array"
      ]
    },
    "next_due_at": {
      "description": "next firing in RFC 3339 UTC, empty while the reminder is disabled or finished",
      "type": "string"
    },
    "nth_weekday": {
      "additionalProperties": false,
      "description": "nth weekday of the month, present when repeat is monthly and month_days is absent",
      "properties": {
        "position": {
          "description": "position of the weekday in the month: 1 = first, 2 = second, 3 = third, 4 = fourth, -1 = last",
          "type": "integer"
        },
        "weekday": {
          "description": "weekday name mon..sun or ISO number 1..7 (1 = Monday)",
          "type": "string"
        }
      },
      "required": [
        "position",
        "weekday"
      ],
      "type": [
        "null",
        "object"
      ]
    },
    "occurrences_fired": {
      "description": "how many occurrences already fired; editing resets it",
      "type": "integer"
    },
    "recipient_ids": {
      "description": "user ids who get the message",
      "items": {
        "type": "string"
      },
      "type": [
        "null",
        "array"
      ]
    },
    "repeat": {
      "description": "once, daily, weekly, monthly or yearly",
      "type": "string"
    },
    "repeat_minutes": {
      "description": "interval in minutes between repeats of an unconfirmed occurrence",
      "type": "integer"
    },
    "schedule_kind": {
      "description": "alias of repeat, kept for older clients",
      "type": "string"
    },
    "space_id": {
      "description": "reminders space the rule belongs to",
      "type": "string"
    },
    "start_date": {
      "description": "first possible day as YYYY-MM-DD in the reminder time zone",
      "type": "string"
    },
    "time": {
      "description": "local wall clock time HH:MM",
      "type": "string"
    },
    "time_zone": {
      "description": "IANA time zone of the schedule",
      "type": "string"
    },
    "until": {
      "description": "last allowed day as YYYY-MM-DD, empty when there is no date end",
      "type": "string"
    },
    "updated_at": {
      "description": "last change time in RFC 3339 UTC",
      "type": "string"
    },
    "weekdays": {
      "description": "weekly days as mon..sun, present when repeat is weekly",
      "items": {
        "type": "string"
      },
      "type": [
        "null",
        "array"
      ]
    }
  },
  "required": [
    "space_id",
    "recipient_ids",
    "id",
    "message",
    "repeat",
    "schedule_kind",
    "interval",
    "start_date",
    "occurrences_fired",
    "finished",
    "time",
    "time_zone",
    "repeat_minutes",
    "enabled",
    "created_at",
    "updated_at"
  ],
  "type": "object"
}
```

### `reminders_list`

List reminders

List the reminders of every space of the current user (or of one space) with the schedule, next due time and, when a reminder has fired and waits for confirmation, its pending occurrence. Returns an empty array when the user has no reminders.

**Доступ:** только чтение. Инструмент ничего не меняет, его можно вызывать свободно.

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "space": {
      "description": "reminders space id or name (see spaces_list). Omit to list every space",
      "type": "string"
    }
  },
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "reminders": {
      "description": "every reminder of the user; empty when there are none",
      "items": {
        "additionalProperties": false,
        "properties": {
          "active_occurrence": {
            "additionalProperties": false,
            "description": "the firing that waits for confirmation, absent otherwise",
            "properties": {
              "completed_at": {
                "description": "confirmation time in RFC 3339 UTC",
                "type": "string"
              },
              "first_sent_at": {
                "description": "time of the first delivered message in RFC 3339 UTC",
                "type": "string"
              },
              "id": {
                "description": "occurrence id (UUID)",
                "type": "string"
              },
              "next_repeat_at": {
                "description": "next repeat time in RFC 3339 UTC, empty when there is none",
                "type": "string"
              },
              "repeat_count": {
                "description": "number of delivered messages, including the first one",
                "type": "integer"
              },
              "scheduled_at": {
                "description": "due time in RFC 3339 UTC",
                "type": "string"
              },
              "status": {
                "description": "active while waiting for confirmation, completed after confirmation, cancelled when the rule changed",
                "type": "string"
              }
            },
            "required": [
              "id",
              "status",
              "scheduled_at",
              "repeat_count"
            ],
            "type": [
              "null",
              "object"
            ]
          },
          "count": {
            "description": "total number of occurrences, zero when there is no count end",
            "type": "integer"
          },
          "created_at": {
            "description": "creation time in RFC 3339 UTC",
            "type": "string"
          },
          "enabled": {
            "description": "whether the reminder is scheduled",
            "type": "boolean"
          },
          "finished": {
            "description": "true when the recurrence has no future occurrence and the last one was confirmed",
            "type": "boolean"
          },
          "finished_at": {
            "description": "finish time in RFC 3339 UTC, empty while not finished",
            "type": "string"
          },
          "id": {
            "description": "reminder id (UUID)",
            "type": "string"
          },
          "interval": {
            "description": "every N days/weeks/months/years",
            "type": "integer"
          },
          "last_error": {
            "description": "last delivery error, empty when the reminder is delivered",
            "type": "string"
          },
          "message": {
            "description": "reminder text",
            "type": "string"
          },
          "month_days": {
            "description": "days of month, present when repeat is monthly and nth_weekday is absent",
            "items": {
              "type": "integer"
            },
            "type": [
              "null",
              "array"
            ]
          },
          "next_due_at": {
            "description": "next firing in RFC 3339 UTC, empty while the reminder is disabled or finished",
            "type": "string"
          },
          "nth_weekday": {
            "additionalProperties": false,
            "description": "nth weekday of the month, present when repeat is monthly and month_days is absent",
            "properties": {
              "position": {
                "description": "position of the weekday in the month: 1 = first, 2 = second, 3 = third, 4 = fourth, -1 = last",
                "type": "integer"
              },
              "weekday": {
                "description": "weekday name mon..sun or ISO number 1..7 (1 = Monday)",
                "type": "string"
              }
            },
            "required": [
              "position",
              "weekday"
            ],
            "type": [
              "null",
              "object"
            ]
          },
          "occurrences_fired": {
            "description": "how many occurrences already fired; editing resets it",
            "type": "integer"
          },
          "recipient_ids": {
            "description": "user ids who get the message",
            "items": {
              "type": "string"
            },
            "type": [
              "null",
              "array"
            ]
          },
          "repeat": {
            "description": "once, daily, weekly, monthly or yearly",
            "type": "string"
          },
          "repeat_minutes": {
            "description": "interval in minutes between repeats of an unconfirmed occurrence",
            "type": "integer"
          },
          "schedule_kind": {
            "description": "alias of repeat, kept for older clients",
            "type": "string"
          },
          "space_id": {
            "description": "reminders space the rule belongs to",
            "type": "string"
          },
          "start_date": {
            "description": "first possible day as YYYY-MM-DD in the reminder time zone",
            "type": "string"
          },
          "time": {
            "description": "local wall clock time HH:MM",
            "type": "string"
          },
          "time_zone": {
            "description": "IANA time zone of the schedule",
            "type": "string"
          },
          "until": {
            "description": "last allowed day as YYYY-MM-DD, empty when there is no date end",
            "type": "string"
          },
          "updated_at": {
            "description": "last change time in RFC 3339 UTC",
            "type": "string"
          },
          "weekdays": {
            "description": "weekly days as mon..sun, present when repeat is weekly",
            "items": {
              "type": "string"
            },
            "type": [
              "null",
              "array"
            ]
          }
        },
        "required": [
          "space_id",
          "recipient_ids",
          "id",
          "message",
          "repeat",
          "schedule_kind",
          "interval",
          "start_date",
          "occurrences_fired",
          "finished",
          "time",
          "time_zone",
          "repeat_minutes",
          "enabled",
          "created_at",
          "updated_at"
        ],
        "type": "object"
      },
      "type": [
        "null",
        "array"
      ]
    }
  },
  "required": [
    "reminders"
  ],
  "type": "object"
}
```

### `reminders_update`

Update a reminder

Change a reminder. Omitted fields keep their current values: message, repeat and its interval/weekdays/month_days/nth_weekday/date/start_date/until/count, time, time_zone, repeat_minutes and enabled are all optional apart from the id. A recurrence change cancels the pending occurrence, resets the fired counter and recomputes the next due time. Returns the updated reminder.

**Доступ:** изменяет. Повторный вызов не создаёт нового состояния; ответ может отличаться (например, «уже подтверждено»).

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "count": {
      "description": "new total number of occurrences 1..10000; removes an until end. Omit to keep the current end",
      "type": [
        "null",
        "integer"
      ]
    },
    "date": {
      "description": "new date of a one-time reminder as YYYY-MM-DD. Omit to keep the current date",
      "type": "string"
    },
    "enabled": {
      "description": "enable or disable the reminder. Omit to keep the current state",
      "type": [
        "null",
        "boolean"
      ]
    },
    "id": {
      "description": "reminder id (UUID) to change",
      "type": "string"
    },
    "interval": {
      "description": "every N days/weeks/months/years, 1..1000. Omit to keep the current interval",
      "type": [
        "null",
        "integer"
      ]
    },
    "message": {
      "description": "new reminder text, 1 to 1500 characters. Omit to keep the current text",
      "type": "string"
    },
    "month_days": {
      "description": "monthly day-of-month schedule: days 1..31. Omit to keep the current schedule",
      "items": {
        "type": "integer"
      },
      "type": [
        "null",
        "array"
      ]
    },
    "nth_weekday": {
      "additionalProperties": false,
      "description": "monthly nth-weekday schedule. Omit to keep the current schedule",
      "properties": {
        "position": {
          "description": "position of the weekday in the month: 1 = first, 2 = second, 3 = third, 4 = fourth, -1 = last",
          "type": "integer"
        },
        "weekday": {
          "description": "weekday name mon..sun or ISO number 1..7 (1 = Monday)",
          "type": "string"
        }
      },
      "required": [
        "position",
        "weekday"
      ],
      "type": [
        "null",
        "object"
      ]
    },
    "recipient_ids": {
      "description": "new recipients: user ids of space members. Omit to keep the current ones",
      "items": {
        "type": "string"
      },
      "type": [
        "null",
        "array"
      ]
    },
    "repeat": {
      "description": "new recurrence kind: once, daily, weekly, monthly or yearly. Omit to keep the current one",
      "type": "string"
    },
    "repeat_minutes": {
      "description": "new repeat interval 1..1440 minutes. Omit to keep the current one",
      "type": [
        "null",
        "integer"
      ]
    },
    "space": {
      "description": "move the reminder to this reminders space (id or name). Omit to keep it where it is",
      "type": "string"
    },
    "start_date": {
      "description": "new first possible day as YYYY-MM-DD. Omit to keep the current one",
      "type": "string"
    },
    "time": {
      "description": "new local wall clock time HH:MM. Omit to keep the current time",
      "type": "string"
    },
    "timezone": {
      "description": "new IANA time zone. Omit to keep the current one",
      "type": "string"
    },
    "until": {
      "description": "new last allowed day as YYYY-MM-DD; removes a count end. Omit to keep the current end",
      "type": "string"
    },
    "weekdays": {
      "description": "weekly schedule: day names mon..sun or ISO numbers 1..7. Omit to keep the current schedule",
      "items": {
        "type": "string"
      },
      "type": [
        "null",
        "array"
      ]
    }
  },
  "required": [
    "id"
  ],
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "active_occurrence": {
      "additionalProperties": false,
      "description": "the firing that waits for confirmation, absent otherwise",
      "properties": {
        "completed_at": {
          "description": "confirmation time in RFC 3339 UTC",
          "type": "string"
        },
        "first_sent_at": {
          "description": "time of the first delivered message in RFC 3339 UTC",
          "type": "string"
        },
        "id": {
          "description": "occurrence id (UUID)",
          "type": "string"
        },
        "next_repeat_at": {
          "description": "next repeat time in RFC 3339 UTC, empty when there is none",
          "type": "string"
        },
        "repeat_count": {
          "description": "number of delivered messages, including the first one",
          "type": "integer"
        },
        "scheduled_at": {
          "description": "due time in RFC 3339 UTC",
          "type": "string"
        },
        "status": {
          "description": "active while waiting for confirmation, completed after confirmation, cancelled when the rule changed",
          "type": "string"
        }
      },
      "required": [
        "id",
        "status",
        "scheduled_at",
        "repeat_count"
      ],
      "type": [
        "null",
        "object"
      ]
    },
    "count": {
      "description": "total number of occurrences, zero when there is no count end",
      "type": "integer"
    },
    "created_at": {
      "description": "creation time in RFC 3339 UTC",
      "type": "string"
    },
    "enabled": {
      "description": "whether the reminder is scheduled",
      "type": "boolean"
    },
    "finished": {
      "description": "true when the recurrence has no future occurrence and the last one was confirmed",
      "type": "boolean"
    },
    "finished_at": {
      "description": "finish time in RFC 3339 UTC, empty while not finished",
      "type": "string"
    },
    "id": {
      "description": "reminder id (UUID)",
      "type": "string"
    },
    "interval": {
      "description": "every N days/weeks/months/years",
      "type": "integer"
    },
    "last_error": {
      "description": "last delivery error, empty when the reminder is delivered",
      "type": "string"
    },
    "message": {
      "description": "reminder text",
      "type": "string"
    },
    "month_days": {
      "description": "days of month, present when repeat is monthly and nth_weekday is absent",
      "items": {
        "type": "integer"
      },
      "type": [
        "null",
        "array"
      ]
    },
    "next_due_at": {
      "description": "next firing in RFC 3339 UTC, empty while the reminder is disabled or finished",
      "type": "string"
    },
    "nth_weekday": {
      "additionalProperties": false,
      "description": "nth weekday of the month, present when repeat is monthly and month_days is absent",
      "properties": {
        "position": {
          "description": "position of the weekday in the month: 1 = first, 2 = second, 3 = third, 4 = fourth, -1 = last",
          "type": "integer"
        },
        "weekday": {
          "description": "weekday name mon..sun or ISO number 1..7 (1 = Monday)",
          "type": "string"
        }
      },
      "required": [
        "position",
        "weekday"
      ],
      "type": [
        "null",
        "object"
      ]
    },
    "occurrences_fired": {
      "description": "how many occurrences already fired; editing resets it",
      "type": "integer"
    },
    "recipient_ids": {
      "description": "user ids who get the message",
      "items": {
        "type": "string"
      },
      "type": [
        "null",
        "array"
      ]
    },
    "repeat": {
      "description": "once, daily, weekly, monthly or yearly",
      "type": "string"
    },
    "repeat_minutes": {
      "description": "interval in minutes between repeats of an unconfirmed occurrence",
      "type": "integer"
    },
    "schedule_kind": {
      "description": "alias of repeat, kept for older clients",
      "type": "string"
    },
    "space_id": {
      "description": "reminders space the rule belongs to",
      "type": "string"
    },
    "start_date": {
      "description": "first possible day as YYYY-MM-DD in the reminder time zone",
      "type": "string"
    },
    "time": {
      "description": "local wall clock time HH:MM",
      "type": "string"
    },
    "time_zone": {
      "description": "IANA time zone of the schedule",
      "type": "string"
    },
    "until": {
      "description": "last allowed day as YYYY-MM-DD, empty when there is no date end",
      "type": "string"
    },
    "updated_at": {
      "description": "last change time in RFC 3339 UTC",
      "type": "string"
    },
    "weekdays": {
      "description": "weekly days as mon..sun, present when repeat is weekly",
      "items": {
        "type": "string"
      },
      "type": [
        "null",
        "array"
      ]
    }
  },
  "required": [
    "space_id",
    "recipient_ids",
    "id",
    "message",
    "repeat",
    "schedule_kind",
    "interval",
    "start_date",
    "occurrences_fired",
    "finished",
    "time",
    "time_zone",
    "repeat_minutes",
    "enabled",
    "created_at",
    "updated_at"
  ],
  "type": "object"
}
```

### `spaces_list`

List spaces

List the spaces of the user: every mini-app (warehouse, reminders) keeps its data in spaces that can be shared with other people. Warehouse and reminder tools take a space argument (id or name); call this to learn the valid values and the members.

**Доступ:** только чтение. Инструмент ничего не меняет, его можно вызывать свободно.

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "app": {
      "description": "only spaces of this mini-app: warehouse or reminders",
      "type": "string"
    }
  },
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "spaces": {
      "description": "spaces the user is a member of",
      "items": {
        "additionalProperties": false,
        "properties": {
          "app": {
            "description": "mini-app of the space: warehouse or reminders",
            "type": "string"
          },
          "id": {
            "description": "space id (UUID); pass it as the space argument of other tools",
            "type": "string"
          },
          "members": {
            "description": "people who share the space",
            "items": {
              "additionalProperties": false,
              "properties": {
                "name": {
                  "description": "display name, or e-mail when there is no name",
                  "type": "string"
                },
                "user_id": {
                  "description": "user id (UUID), for example a reminder recipient",
                  "type": "string"
                }
              },
              "required": [
                "user_id",
                "name"
              ],
              "type": "object"
            },
            "type": [
              "null",
              "array"
            ]
          },
          "name": {
            "description": "space name",
            "type": "string"
          },
          "personal": {
            "description": "the user's own default space of this mini-app",
            "type": "boolean"
          }
        },
        "required": [
          "id",
          "name",
          "app",
          "personal",
          "members"
        ],
        "type": "object"
      },
      "type": [
        "null",
        "array"
      ]
    }
  },
  "required": [
    "spaces"
  ],
  "type": "object"
}
```

### `whoami`

Who am I

Return the Cublyverse user behind the personal access token: principal id, profile fields, time zone and the scopes of the token. Use it to learn the default time zone before creating a reminder and to check what the token is allowed to do.

**Доступ:** только чтение. Инструмент ничего не меняет, его можно вызывать свободно.

**Входная схема**

```json
{
  "additionalProperties": false,
  "type": "object"
}
```

**Выходная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "display_name": {
      "description": "display name from the SSO profile, empty when unavailable",
      "type": "string"
    },
    "email": {
      "description": "e-mail from the SSO profile, empty when the provider does not send one",
      "type": "string"
    },
    "principal_id": {
      "description": "stable user id (UUID) that owns every object this token can reach",
      "type": "string"
    },
    "scopes": {
      "description": "scopes granted to the token used for this call",
      "items": {
        "type": "string"
      },
      "type": [
        "null",
        "array"
      ]
    },
    "time_zone": {
      "description": "IANA time zone of the user, the default for new reminders",
      "type": "string"
    }
  },
  "required": [
    "principal_id",
    "time_zone",
    "scopes"
  ],
  "type": "object"
}
```

<!-- END GENERATED MCP TOOLS -->

Блок между маркерами генерируется из кода командой

```bash
go run ./cmd/cublyverse docs mcp --write
```

`make gen` делает это автоматически, а `make check` падает, если
`docs/MCP.md` разошёлся с зарегистрированными инструментами: описание, схемы и
аннотации не могут «отстать» от кода.

### Как читать аннотации

- **только чтение** (`readOnlyHint: true`) — инструмент ничего не меняет;
- **изменяет** (`destructiveHint: false`) — создаёт или переписывает данные без
  удаления;
- **изменяет и удаляет** (`destructiveHint: true`) — может удалить напоминание.

Сервер всегда выставляет `openWorldHint: false`: всё, что делают инструменты,
замкнуто в данных пользователя.

## Примеры вызовов

Ниже — `tools/call` для каждого инструмента. Все запросы идут на
`POST /mcp` с `Authorization: Bearer cv_pat_…`, `Content-Type:
application/json` и `Accept: application/json, text/event-stream`. Ответ
приходит как обычный JSON-RPC результат; полезная нагрузка лежит в
`result.structuredContent` (в примерах показана только она).

### Кто я (`whoami`)

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {"name": "whoami", "arguments": {}}
}
```

```json
{
  "principal_id": "6f1c0a3e-5b9d-4a1f-9c2b-7e8d0f1a2b3c",
  "email": "user@example.com",
  "display_name": "Иван Петров",
  "time_zone": "Europe/Moscow",
  "scopes": ["profile:read", "reminders:read", "reminders:write"]
}
```

Используйте `time_zone` как значение по умолчанию, если не хотите спрашивать
часовой пояс у пользователя. `scopes` показывает, что разрешено этому токену.

### Список напоминаний

Запрос:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {"name": "reminders_list", "arguments": {}}
}
```

Ответ (структурированное содержимое, фрагмент):

```json
{
  "reminders": [
    {
      "id": "6f1c0a3e-5b9d-4a1f-9c2b-7e8d0f1a2b3c",
      "message": "Выпить таблетку",
      "schedule_kind": "weekly",
      "weekdays": ["mon", "wed", "fri"],
      "time": "09:30",
      "time_zone": "Europe/Moscow",
      "repeat_minutes": 10,
      "enabled": true,
      "next_due_at": "2026-01-12T06:30:00Z"
    }
  ]
}
```

### Одно напоминание (`reminders_get`)

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "tools/call",
  "params": {
    "name": "reminders_get",
    "arguments": {"id": "6f1c0a3e-5b9d-4a1f-9c2b-7e8d0f1a2b3c"}
  }
}
```

Ответ — один объект напоминания в той же форме, что элемент `reminders` из
списка. Если напоминание чужое или удалено, инструмент возвращает
`isError: true` с текстом `reminder not found: …`; существование чужого
напоминания не раскрывается.

### Создание напоминания (`reminders_create`)

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "tools/call",
  "params": {
    "name": "reminders_create",
    "arguments": {
      "message": "Полить цветы",
      "weekdays": ["tue", "sat"],
      "time": "19:00"
    }
  }
}
```

Часовой пояс не указан — берётся из профиля пользователя (`whoami.time_zone`).
Повтор по умолчанию — 10 минут, напоминание включено. `weekdays` принимает и
имена (`mon`..`sun`), и номера ISO (`1`..`7`); для месячного расписания вместо
`weekdays` передаётся `month_days` (1..31, несуществующий день = последний день
месяца) или `nth_weekday`, например второй вторник:

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "tools/call",
  "params": {
    "name": "reminders_create",
    "arguments": {
      "message": "Сдать отчёт",
      "repeat": "monthly",
      "nth_weekday": {"position": 2, "weekday": "tue"},
      "start_date": "2026-02-01",
      "until": "2026-12-31",
      "time": "18:00"
    }
  }
}
```

`repeat` — `once`, `daily`, `weekly`, `monthly` или `yearly`; `interval` делает
повтор «каждые N» дней/недель/месяцев/лет от `start_date`; `date` задаёт день
разового напоминания; `until` или `count` останавливают повтор. Старая форма без
`repeat` (только `weekdays` или `month_days`) работает как раньше.

### Изменение напоминания (`reminders_update`)

```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "method": "tools/call",
  "params": {
    "name": "reminders_update",
    "arguments": {
      "id": "6f1c0a3e-5b9d-4a1f-9c2b-7e8d0f1a2b3c",
      "time": "21:00",
      "repeat_minutes": 30
    }
  }
}
```

Передавайте только то, что меняется: остальные поля адаптер берёт из текущего
напоминания. Правка отменяет ожидающее срабатывание, сбрасывает счётчик
срабатываний и пересчитывает `next_due_at` от текущего момента. Выключение —
`"enabled": false`; включение обратно — `"enabled": true`. В одном вызове
нельзя смешивать `weekdays`, `month_days` и `nth_weekday`; `until` и `count`
взаимоисключающие.

### Удаление напоминания (`reminders_delete`)

```json
{
  "jsonrpc": "2.0",
  "id": 5,
  "method": "tools/call",
  "params": {
    "name": "reminders_delete",
    "arguments": {"id": "6f1c0a3e-5b9d-4a1f-9c2b-7e8d0f1a2b3c"}
  }
}
```

Ответ: `{"id": "…", "deleted": true}`. Повторное удаление даёт
`isError: true` и `reminder not found: …` — клиент должен считать это
успешным результатом «уже удалено», а не сбоем транспорта.

### Подтверждение срабатывания (`reminders_complete`)

```json
{
  "jsonrpc": "2.0",
  "id": 6,
  "method": "tools/call",
  "params": {"name": "reminders_complete", "arguments": {"id": "6f1c0a3e-5b9d-4a1f-9c2b-7e8d0f1a2b3c"}}
}
```

Это та же операция, что кнопка «✅ Выполнено» в чате и
`POST /api/v1/reminders/{id}/complete`: из нескольких одновременных
подтверждений побеждает ровно одно. Если подтверждать нечего (не срабатывало,
уже подтверждено, правка отменила срабатывание), инструмент возвращает ошибку
`nothing to confirm: …`.

### Склад: закупки и проекты

Инструменты `craft_*` работают с одним складом: он задаётся аргументом `space`,
если складов несколько. Примеры ниже — те же `tools/call`, что и выше.

Добавить в wishlist позицию каталога и свободный текст:

```json
{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"craft_wishlist_add","arguments":{"catalog_item_id":"7b2c1f4e-0a3d-4c8e-9f61-2d5a8b7c6e10","qty_wanted":2,"priority":2,"note":"на подарок"}}}
```

```json
{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"craft_wishlist_add","arguments":{"title":"Лента атласная, 1 см"}}}
```

Список, правка и удаление (по умолчанию видны только `wanted`; `all` — все):

```json
{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"craft_wishlist_list","arguments":{"status":"all"}}}
```

```json
{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"craft_wishlist_update","arguments":{"wish_id":"3f0b9a2c-6d41-4e7b-8c55-1a2b3c4d5e6f","status":"ordered","priority":1}}}
```

```json
{"jsonrpc":"2.0","id":14,"method":"tools/call","params":{"name":"craft_wishlist_remove","arguments":{"wish_id":"3f0b9a2c-6d41-4e7b-8c55-1a2b3c4d5e6f"}}}
```

Ссылки на магазины (url только http(s)):

```json
{"jsonrpc":"2.0","id":15,"method":"tools/call","params":{"name":"craft_wishlist_link_add","arguments":{"wish_id":"3f0b9a2c-6d41-4e7b-8c55-1a2b3c4d5e6f","url":"https://shop.example/beads","shop_name":"Магазин","price":12.5,"currency":"RUB","in_stock":true}}}
```

```json
{"jsonrpc":"2.0","id":16,"method":"tools/call","params":{"name":"craft_wishlist_link_remove","arguments":{"wish_id":"3f0b9a2c-6d41-4e7b-8c55-1a2b3c4d5e6f","link_id":"9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d"}}}
```

Покупка: статус `bought`, а с `add_to_stock` купленное количество ещё и
попадает в остаток (повторный вызов не добавляет его дважды):

```json
{"jsonrpc":"2.0","id":17,"method":"tools/call","params":{"name":"craft_wishlist_mark_bought","arguments":{"wish_id":"3f0b9a2c-6d41-4e7b-8c55-1a2b3c4d5e6f","add_to_stock":true}}}
```

Проекты: создать, запланировать материал, прочитать проект.

```json
{"jsonrpc":"2.0","id":18,"method":"tools/call","params":{"name":"craft_project_create","arguments":{"name":"Ёлка","notes":"подарок"}}}
```

```json
{"jsonrpc":"2.0","id":19,"method":"tools/call","params":{"name":"craft_project_set_material","arguments":{"project_id":"5c4d3e2f-1a0b-4c9d-8e7f-6a5b4c3d2e1f","item_id":"7b2c1f4e-0a3d-4c8e-9f61-2d5a8b7c6e10","qty_planned":20}}}
```

```json
{"jsonrpc":"2.0","id":20,"method":"tools/call","params":{"name":"craft_project_get","arguments":{"project_id":"5c4d3e2f-1a0b-4c9d-8e7f-6a5b4c3d2e1f"}}}
```

```json
{"jsonrpc":"2.0","id":21,"method":"tools/call","params":{"name":"craft_project_list","arguments":{}}}
```

Списать материал на проект: остаток уменьшается, `qty_used` растёт, всё в
одной транзакции; при нехватке — ошибка и ничего не меняется.

```json
{"jsonrpc":"2.0","id":22,"method":"tools/call","params":{"name":"craft_project_use_material","arguments":{"project_id":"5c4d3e2f-1a0b-4c9d-8e7f-6a5b4c3d2e1f","item_id":"7b2c1f4e-0a3d-4c8e-9f61-2d5a8b7c6e10","qty":8}}}
```

Дефицит проекта — в wishlist (`shortage = max(0, qty_planned - qty_used -
stock)`; уже добавленное пропускается):

```json
{"jsonrpc":"2.0","id":23,"method":"tools/call","params":{"name":"craft_project_shortage_to_wishlist","arguments":{"project_id":"5c4d3e2f-1a0b-4c9d-8e7f-6a5b4c3d2e1f"}}}
```

### Сырой HTTP-запрос

```bash
curl -sS https://cublyverse.example.com/mcp \
  -H "Authorization: Bearer cv_pat_…" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'
```

## Ошибки

| Что произошло | Ответ |
|---|---|
| нет заголовка `Authorization`, токен неизвестен, просрочен или отозван | `401`, тело `{"code":"unauthorized","message":"…"}` |
| запрос с cookie-сессией вместо PAT | `401` (MCP не принимает сессии) |
| слишком много неудачных попыток с одного IP | `401`; попытка до базы не доходит |
| `GET /mcp` (standalone SSE) | `405`, заголовок `Allow: POST` |
| неверный JSON-RPC, неизвестный инструмент, невалидные аргументы по схеме | ошибка JSON-RPC (`-32600`/`-32601`/`-32602`) |
| недостаточно прав у токена | результат инструмента с `isError: true`, текст `insufficient scope: this tool requires …` |
| напоминание не найдено или чужое | `isError: true`, `reminder not found: …` |
| подтверждать нечего | `isError: true`, `nothing to confirm: …` |
| ошибка валидации правила | `isError: true`, русский текст сервиса (например, «Время должно быть в формате ЧЧ:ММ») |

Ошибки инструментов приходят как результат (`isError: true`), а не как
JSON-RPC-ошибка: модель должна их видеть и исправлять аргументы сама.

## Подключение

### Claude Code

```bash
claude mcp add --transport http cublyverse https://cublyverse.example.com/mcp \
  --header "Authorization: Bearer cv_pat_…"
```

Проверить: `claude mcp list`; токен должен иметь хотя бы `profile:read` и нужные
`reminders:*` области.

### Claude Desktop

В `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "cublyverse": {
      "type": "http",
      "url": "https://cublyverse.example.com/mcp",
      "headers": {
        "Authorization": "Bearer cv_pat_…"
      }
    }
  }
}
```

### Любой другой клиент

Нужны три вещи: URL `https://<хост>/mcp`, транспорт Streamable HTTP и заголовок
`Authorization: Bearer cv_pat_…` в каждом запросе. Отдельного SSE-соединения
открывать не нужно и не получится: сервер stateless.

## Проверка

```bash
# какие инструменты зарегистрированы
go run ./cmd/cublyverse docs mcp --names

# тесты инструментов на реальном PostgreSQL (нужен Docker)
go test ./internal/mcpserver/... ./internal/app/ -run 'MCP|ReminderTools' -v

# HTTP-уровень: реальный Streamable HTTP клиент, PAT и отказы
go test ./internal/app/ -run TestMCP -v
```
