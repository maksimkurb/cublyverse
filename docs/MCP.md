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

Токен создаётся в WebApp на экране «Доступ по токенам» (`docs/WEBAPP.md`) или
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

### Области доступа

| Scope | Что разрешает |
|---|---|
| `profile:read` | `whoami`; выдается неявно, если у токена есть любой другой scope |
| `reminders:read` | `reminders_list`, `reminders_get` |
| `reminders:write` | `reminders_create`, `reminders_update`, `reminders_delete`, `reminders_complete` |

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
| `reminders_complete` | Complete a reminder occurrence | изменяет |
| `reminders_create` | Create a reminder | изменяет |
| `reminders_delete` | Delete a reminder | изменяет и удаляет |
| `reminders_get` | Get a reminder | только чтение |
| `reminders_list` | List reminders | только чтение |
| `reminders_update` | Update a reminder | изменяет |
| `whoami` | Who am I | только чтение |

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
    "created_at": {
      "description": "creation time in RFC 3339 UTC",
      "type": "string"
    },
    "enabled": {
      "description": "whether the reminder is scheduled",
      "type": "boolean"
    },
    "id": {
      "description": "reminder id (UUID)",
      "type": "string"
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
      "description": "days of month, present when schedule_kind is monthly",
      "items": {
        "type": "integer"
      },
      "type": [
        "null",
        "array"
      ]
    },
    "next_due_at": {
      "description": "next firing in RFC 3339 UTC, empty while the reminder is disabled",
      "type": "string"
    },
    "repeat_minutes": {
      "description": "interval in minutes between repeats of an unconfirmed occurrence",
      "type": "integer"
    },
    "schedule_kind": {
      "description": "weekly or monthly",
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
    "updated_at": {
      "description": "last change time in RFC 3339 UTC",
      "type": "string"
    },
    "weekdays": {
      "description": "weekly days as mon..sun, present when schedule_kind is weekly",
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
    "id",
    "message",
    "schedule_kind",
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

Create a recurring reminder for the current user. Give either weekdays (mon..sun or 1..7) or month_days (1..31), a local time HH:MM and optionally an IANA time zone; without a time zone the user's profile time zone is used. The reminder is enabled by default and repeats an unconfirmed occurrence every repeat_minutes (default 10). Returns the stored reminder with its next due time in UTC.

**Доступ:** изменяет. Каждый вызов создаёт новое состояние.

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
    "enabled": {
      "description": "create the reminder already scheduled. Default true",
      "type": [
        "null",
        "boolean"
      ]
    },
    "message": {
      "description": "reminder text in the user's language, 1 to 1500 characters",
      "type": "string"
    },
    "month_days": {
      "description": "monthly schedule: days of month 1..31. A day the month does not have means its last day. Use either weekdays or month_days",
      "items": {
        "type": "integer"
      },
      "type": [
        "null",
        "array"
      ]
    },
    "repeat_minutes": {
      "description": "how often an unconfirmed occurrence repeats, 1..1440 minutes. Default 10",
      "type": [
        "null",
        "integer"
      ]
    },
    "time": {
      "description": "local wall clock time as HH:MM in 24-hour form, for example 09:30",
      "type": "string"
    },
    "timezone": {
      "description": "IANA time zone such as Europe/Moscow. Defaults to the user's profile time zone",
      "type": "string"
    },
    "weekdays": {
      "description": "weekly schedule: day names mon,tue,wed,thu,fri,sat,sun or ISO numbers 1..7 (1 = Monday). Use either weekdays or month_days",
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
    "created_at": {
      "description": "creation time in RFC 3339 UTC",
      "type": "string"
    },
    "enabled": {
      "description": "whether the reminder is scheduled",
      "type": "boolean"
    },
    "id": {
      "description": "reminder id (UUID)",
      "type": "string"
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
      "description": "days of month, present when schedule_kind is monthly",
      "items": {
        "type": "integer"
      },
      "type": [
        "null",
        "array"
      ]
    },
    "next_due_at": {
      "description": "next firing in RFC 3339 UTC, empty while the reminder is disabled",
      "type": "string"
    },
    "repeat_minutes": {
      "description": "interval in minutes between repeats of an unconfirmed occurrence",
      "type": "integer"
    },
    "schedule_kind": {
      "description": "weekly or monthly",
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
    "updated_at": {
      "description": "last change time in RFC 3339 UTC",
      "type": "string"
    },
    "weekdays": {
      "description": "weekly days as mon..sun, present when schedule_kind is weekly",
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
    "id",
    "message",
    "schedule_kind",
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
    "created_at": {
      "description": "creation time in RFC 3339 UTC",
      "type": "string"
    },
    "enabled": {
      "description": "whether the reminder is scheduled",
      "type": "boolean"
    },
    "id": {
      "description": "reminder id (UUID)",
      "type": "string"
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
      "description": "days of month, present when schedule_kind is monthly",
      "items": {
        "type": "integer"
      },
      "type": [
        "null",
        "array"
      ]
    },
    "next_due_at": {
      "description": "next firing in RFC 3339 UTC, empty while the reminder is disabled",
      "type": "string"
    },
    "repeat_minutes": {
      "description": "interval in minutes between repeats of an unconfirmed occurrence",
      "type": "integer"
    },
    "schedule_kind": {
      "description": "weekly or monthly",
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
    "updated_at": {
      "description": "last change time in RFC 3339 UTC",
      "type": "string"
    },
    "weekdays": {
      "description": "weekly days as mon..sun, present when schedule_kind is weekly",
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
    "id",
    "message",
    "schedule_kind",
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

List every reminder of the current user with its schedule, next due time and, when a reminder has fired and waits for confirmation, its pending occurrence. Returns an empty array when the user has no reminders.

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
          "created_at": {
            "description": "creation time in RFC 3339 UTC",
            "type": "string"
          },
          "enabled": {
            "description": "whether the reminder is scheduled",
            "type": "boolean"
          },
          "id": {
            "description": "reminder id (UUID)",
            "type": "string"
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
            "description": "days of month, present when schedule_kind is monthly",
            "items": {
              "type": "integer"
            },
            "type": [
              "null",
              "array"
            ]
          },
          "next_due_at": {
            "description": "next firing in RFC 3339 UTC, empty while the reminder is disabled",
            "type": "string"
          },
          "repeat_minutes": {
            "description": "interval in minutes between repeats of an unconfirmed occurrence",
            "type": "integer"
          },
          "schedule_kind": {
            "description": "weekly or monthly",
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
          "updated_at": {
            "description": "last change time in RFC 3339 UTC",
            "type": "string"
          },
          "weekdays": {
            "description": "weekly days as mon..sun, present when schedule_kind is weekly",
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
          "id",
          "message",
          "schedule_kind",
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

Change a reminder. Omitted fields keep their current values: message, time, weekdays or month_days, time_zone, repeat_minutes and enabled are all optional apart from the id. A change cancels the pending occurrence and recomputes the next due time. Returns the updated reminder.

**Доступ:** изменяет. Повторный вызов не создаёт нового состояния; ответ может отличаться (например, «уже подтверждено»).

**Входная схема**

```json
{
  "additionalProperties": false,
  "properties": {
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
    "message": {
      "description": "new reminder text, 1 to 1500 characters. Omit to keep the current text",
      "type": "string"
    },
    "month_days": {
      "description": "monthly schedule: days of month 1..31. Omit to keep the current schedule; do not combine with weekdays",
      "items": {
        "type": "integer"
      },
      "type": [
        "null",
        "array"
      ]
    },
    "repeat_minutes": {
      "description": "new repeat interval 1..1440 minutes. Omit to keep the current one",
      "type": [
        "null",
        "integer"
      ]
    },
    "time": {
      "description": "new local wall clock time HH:MM. Omit to keep the current time",
      "type": "string"
    },
    "timezone": {
      "description": "new IANA time zone. Omit to keep the current one",
      "type": "string"
    },
    "weekdays": {
      "description": "weekly schedule: day names mon..sun or ISO numbers 1..7. Omit to keep the current schedule; do not combine with month_days",
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
    "created_at": {
      "description": "creation time in RFC 3339 UTC",
      "type": "string"
    },
    "enabled": {
      "description": "whether the reminder is scheduled",
      "type": "boolean"
    },
    "id": {
      "description": "reminder id (UUID)",
      "type": "string"
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
      "description": "days of month, present when schedule_kind is monthly",
      "items": {
        "type": "integer"
      },
      "type": [
        "null",
        "array"
      ]
    },
    "next_due_at": {
      "description": "next firing in RFC 3339 UTC, empty while the reminder is disabled",
      "type": "string"
    },
    "repeat_minutes": {
      "description": "interval in minutes between repeats of an unconfirmed occurrence",
      "type": "integer"
    },
    "schedule_kind": {
      "description": "weekly or monthly",
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
    "updated_at": {
      "description": "last change time in RFC 3339 UTC",
      "type": "string"
    },
    "weekdays": {
      "description": "weekly days as mon..sun, present when schedule_kind is weekly",
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
    "id",
    "message",
    "schedule_kind",
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
месяца).

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
напоминания. Правка отменяет ожидающее срабатывание и пересчитывает
`next_due_at` от текущего момента. Выключение — `"enabled": false`; включение
обратно — `"enabled": true`. Нельзя одновременно передать `weekdays` и
`month_days`.

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
