# AGENTS.md — qubership-apihub-service

This directory contains all Go application code for qubership-apihub-backend.

## Build & Test
```bash
go build -o apihub-service .
go test ./...
```

## Package Layout
- `Service.go` — main() entrypoint with all dependency wiring and route registration
- `controller/` — HTTP handlers; NO business logic here
- `service/` — business logic layer
- `repository/` — database access via go-pg v10
- `entity/` — database model structs (go-pg tags)
- `view/` — API request/response DTOs (JSON tags)
- `exception/` — CustomError types with HTTP status codes
- `security/` — auth middleware (JWT, SAML, OIDC)
- `resources/migrations/` — SQL migration files (numbered up/down pairs)

## Adding Code
When adding a new feature, follow this order:
1. view/ (DTOs) → 2. entity/ (if DB) → 3. migrations/ (if DB) → 4. repository/ → 5. service/ → 6. controller/ → 7. Service.go (routes + wiring)

## Key Rules
- Every component: interface + private impl + New* constructor
- Errors: exception.CustomError{Status, Code, Message}
- Context: context.SecurityContext (not stdlib)
- Logging: logrus as "log"; INFO=writes, ERROR=errors
- Never put business logic in controllers
- Never access repositories from controllers directly
