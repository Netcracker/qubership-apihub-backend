# AGENTS.md — Service Layer

Business logic layer. Each service is an interface + private implementation.

## Responsibilities
- Business rule validation
- Orchestrating repository calls
- Data transformation (entity ↔ view)
- Logging significant operations

## Rules
- All business logic lives here
- May call other services
- May call repositories
- Returns view types or errors
- Use exception.CustomError for business errors
- Log INFO for write operations, ERROR for all errors
- Async operations: log start with ID, prefix all logs, log completion
