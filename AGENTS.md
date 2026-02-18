# AGENTS.md — AI Agent Instructions for qubership-apihub-backend

## Project Overview

**qubership-apihub-backend** is the main backend microservice of the qubership-apihub API Registry platform.
Written in Go 1.24, it provides REST API for managing API specifications, packages, versions, and operations.

Repository: `github.com/Netcracker/qubership-apihub-backend`

## Tech Stack

| Component | Technology |
|-----------|-----------|
| Language | Go 1.24 |
| HTTP Router | gorilla/mux |
| Database | PostgreSQL (go-pg/pg/v10 ORM) |
| Distributed Cache | Olric |
| Object Storage | MinIO/S3 |
| Authentication | JWT + SAML + OIDC + LDAP |
| Logging | logrus |
| Metrics | Prometheus |
| AI Integration | OpenAI + MCP (Model Context Protocol) |
| Container | Docker (Alpine-based) |

## Project Structure

```
qubership-apihub-service/          # All Go application code
├── Service.go                      # Entrypoint — main(), dependency wiring, route registration
├── controller/                     # HTTP handlers (REST API endpoints)
├── service/                        # Business logic
│   └── cleanup/                    # Scheduled cleanup jobs
├── repository/                     # Data access layer (PostgreSQL)
├── entity/                         # Database models (go-pg structs)
├── view/                           # API request/response DTOs
├── exception/                      # Error types and codes
├── security/                       # Auth middleware, JWT, IdP providers
│   └── idp/providers/              # SAML, OIDC implementations
├── db/                             # Database connection provider
├── cache/                          # Olric distributed cache
├── migration/                      # Data migration (controller/service/repository)
├── metrics/                        # Prometheus metrics
├── utils/                          # Shared utilities
├── middleware/                     # HTTP middleware
├── archive/                        # ZIP archive handling
├── client/                         # External HTTP clients
├── context/                        # Security context (NOT stdlib context)
├── resources/migrations/           # SQL migration files (numbered up/down pairs)
└── tests/                          # Test files
docs/
├── api/                            # OpenAPI specifications (APIHUB_API.yaml, Admin API.yaml)
├── development_guide.md            # Coding standards and conventions
└── local_development/              # Docker-compose files for local dev
```

## Architecture

Strict layered architecture with unidirectional dependencies:

```
Controller → Service → Repository → Entity
     ↓           ↓
    View        View
```

- **Controller**: HTTP handling, permission checks, delegates to service
- **Service**: Business logic, validation, orchestration
- **Repository**: Database queries via go-pg ORM
- **Entity**: Database table mappings (structs with `pg` tags)
- **View**: API DTOs (request/response), JSON serialization
- **Exception**: Custom errors with HTTP status codes

## Code Patterns

### Interface-First Design
Every component defines a public interface + private implementation:
```go
type PackageService interface { ... }
type packageServiceImpl struct { ... }
func NewPackageService(deps...) PackageService { return &packageServiceImpl{...} }
```

### Dependency Injection
Manual constructor injection in `Service.go`. No DI framework.

### Error Handling
Use `exception.CustomError` with Status, Code, Message, Params.
Never ignore errors.

### Function Parameters
Required params as separate arguments, optional via struct.

### Deprecated Code
Append `_deprecated` suffix to deprecated methods/functions.

### Logging
- logrus imported as `log`
- INFO: all write operations (POST/PUT/DELETE), major events
- ERROR: all errors
- Async ops: log start with ID, prefix all logs with operation ID, log end

## API Conventions

- API-first: design OpenAPI spec before implementing
- No breaking changes without URL version bump (`/api/v2/` → `/api/v3/`)
- Conventional commits for PR titles: `feat:`, `fix:`, `refactor:`, `docs:`, `chore:`
- Squash commits on merge

## Database

- PostgreSQL via go-pg v10 ORM
- Migrations in `resources/migrations/` — numbered pairs (`N_desc.up.sql` + `N_desc.down.sql`)
- Always create both up AND down migrations
- Soft deletes used for some entities (cleanup jobs purge old records)
- Distributed locking via PostgreSQL advisory locks

## Building & Running

```bash
# Build binary
go build -o apihub-service ./qubership-apihub-service/

# Build Docker
docker build -t apihub-backend .

# Local dev: start dependencies
cd docs/local_development/docker-compose/DB && docker-compose up -d
```

## Testing

- Unit tests: `*_test.go` files alongside source
- E2E tests: Postman collections via GitHub Actions
- Run unit tests: `cd qubership-apihub-service && go test ./...`

## Business Domain Glossary

| Term | Definition |
|------|-----------|
| workspace | Top-level group for project/team separation |
| group | Logical grouping of packages within a workspace |
| package | Contains published API documents for a service |
| dashboard | Virtual package with links to other packages (no own docs) |
| version | Package version (Draft → Release → Archived) |
| reference | Link between published versions |
| build | Processing API specs: dereference, comparison, search index |
| BWC | Backward Compatibility analysis between releases |
| operation | Single API operation (endpoint) within a specification |

## Files to Never Modify Without Care

- `Service.go` — entrypoint with all dependency wiring; changes here affect everything
- `resources/migrations/*.sql` — already-applied migrations must never be altered
- `config.template.yaml` — configuration reference document
- `docs/api/*.yaml` — OpenAPI specs require API review process
