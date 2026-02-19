# CLAUDE.md — Instructions for Claude Code

## Project

qubership-apihub-backend — Go 1.24 backend microservice for API Registry platform.
Module: `github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service`

## Quick Reference

- **Entrypoint**: `qubership-apihub-service/Service.go`
- **Build**: `cd qubership-apihub-service && go build -o apihub-service .`
- **Test**: `cd qubership-apihub-service && go test ./...`
- **Lint**: `golangci-lint run --timeout 10m` (config in `.github/linters/.golangci.yml`)
- **Docker**: `docker build -t apihub-backend .`

## Architecture

Layered: Controller → Service → Repository → Entity, with View as DTOs.
All in `qubership-apihub-service/`. Dependencies wired manually in `Service.go` main().

## Key Patterns

1. **Interface + private impl + constructor**: `type FooService interface{...}`, `type fooServiceImpl struct{...}`, `func NewFooService() FooService`
2. **Error handling**: `exception.CustomError{Status, Code, Message, Params}` — never swallow errors
3. **Context**: Use `context.SecurityContext` (project's own), NOT stdlib context. Import stdlib as `stdctx "context"` when needed
4. **Logging**: `log "github.com/sirupsen/logrus"` — INFO for writes, ERROR for errors, DEBUG for details
5. **Parameters**: required as separate args, optional via struct
6. **Deprecation**: `_deprecated` suffix on old methods

## Database

- PostgreSQL + go-pg v10
- Entities in `entity/` with `pg:"table_name"` struct tags
- Migrations in `resources/migrations/` — always create both `N_name.up.sql` and `N_name.down.sql`
- Never modify already-deployed migrations — create new ones

## API Rules

- API-first: OpenAPI spec in `docs/api/` before code
- No breaking changes without URL version bump (`/api/v2/` → `/api/v3/`)
- Controllers: parse request → check permissions → call service → respond
- PR titles: conventional commits (`feat:`, `fix:`, `refactor:`)

## Things to Avoid

- Don't put business logic in controllers
- Don't access repositories directly from controllers
- Don't modify existing SQL migrations
- Don't commit secrets (keys, passwords, tokens)
- Don't ignore errors
- Don't add GET request logging at INFO level (too noisy)

## Domain Terms

workspace → top-level group | package → API docs container | version → Draft/Release/Archived | build → spec processing | BWC → backward compatibility | dashboard → virtual package with links
