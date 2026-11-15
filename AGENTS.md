# Agent guide

Read `docs/PLAN.md` (architecture, decisions) and `docs/STORIES.md` (work items
with acceptance criteria) before changing anything.

## Rules

- Work story by story, in order. When a story is done, set its status to ✅ in
  `docs/STORIES.md`.
- `api/openapi.yaml` is the source of truth for the HTTP API. Change the spec
  first, run `make gen`, then implement. Never hand-edit generated code
  (`internal/httpapi/gen`, `internal/store/db`, `web/src/api/schema.d.ts`).
- Business logic lives in `internal/features/<feature>` services. HTTP
  handlers, bot handlers and MCP tools are thin adapters over the same service.
- Every data access is scoped by the authenticated user (`PrincipalID`).
- SQL goes through sqlc queries; schema changes go through new goose
  migrations (never edit an applied migration).
- `unibot/` is our own framework code: modify it in place when a story says
  so, keep its tests passing. Feature code must not import
  `unibot/platform/*`.
- Go: stdlib first (`net/http`, `log/slog`, `flag`). Add a dependency only when
  the plan names it or there is no reasonable stdlib way. Wrap errors with
  `%w`. Table-driven tests.
- User-facing text (bot messages, WebApp) is in Russian. Code, comments,
  commits and logs are in English.
- Never commit secrets. Configuration comes from env; document new variables
  in `.env.example`.
- `make check` must pass before a story is considered done.
- Documentation is part of done: every story that adds a migration, API operation, MCP tool, env variable or feature updates the matching file in `docs/` in the same change (see M7-1 for the list). Every OpenAPI operation needs summary/description; every MCP tool needs a description and an entry in `docs/MCP.md`. A new file in `docs/` is listed in `docs/README.md`, a new migration in `docs/DATABASE.md`, and a new Makefile target in `docs/DEVELOPMENT.md`: `make check-docs` fails otherwise.

## Commands

```text
make gen     regenerate Go server/types, sqlc, TS client
make check   gen (no diff) + vet + lint + tests (Go and web)
make build   build web, then the Go binary
make dev     run backend with live config from .env
```
