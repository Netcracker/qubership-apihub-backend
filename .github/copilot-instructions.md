# GitHub Copilot Instructions for qubership-apihub-backend

## Project Context

This is a Go 1.24 backend microservice for qubership-apihub (API Registry platform).
All code is in `qubership-apihub-service/` with layered architecture: Controller → Service → Repository → Entity.

## Code Style

- **Interface-first**: define interface, private impl struct, `New*` constructor returning interface
- **Error handling**: use `exception.CustomError{Status, Code, Message, Params}`, never ignore errors
- **Logging**: `log "github.com/sirupsen/logrus"` — INFO for write ops, ERROR for errors
- **Context**: use project's `context.SecurityContext`, not stdlib context
- **Parameters**: required as separate args, optional via struct
- **Deprecated code**: suffix `_deprecated`
- **No business logic in controllers** — delegate to service layer
- **No direct repository access from controllers**

## When Generating Go Code

- Follow the existing interface + impl + constructor pattern
- Use go-pg v10 for database operations
- Use gorilla/mux for routing
- Handle errors explicitly with `exception.CustomError`
- Use `utils.RespondWithJson()` and `utils.RespondWithCustomError()` for HTTP responses
- Check permissions with `roleService.HasRequiredPermissions()` in controllers
- Create context with `ctx := context.Create(r)`

## When Generating SQL Migrations

- Place in `qubership-apihub-service/resources/migrations/`
- Always create both `.up.sql` and `.down.sql` files
- Use sequential numbering
- Use `IF EXISTS` / `IF NOT EXISTS` for safety

## API Conventions

- URL versioning: `/api/v2/...`, `/api/v3/...`
- No breaking changes without version bump
- API-first: design OpenAPI spec before implementation
- Conventional commits for PR titles

## Domain Language

- workspace = top-level group
- package = API docs container with versions
- version = Draft → Release → Archived
- build = specification processing pipeline
- BWC = backward compatibility
- dashboard = virtual package (links only)
- operation = single API endpoint
