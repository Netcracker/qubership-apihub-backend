# New REST API Endpoint

Create a new REST API endpoint for qubership-apihub-backend following the project's layered architecture.

## Steps
1. Add view types (request/response DTOs) in `qubership-apihub-service/view/`
2. Add entity types in `qubership-apihub-service/entity/` if DB changes needed
3. Create SQL migration (both up and down) in `qubership-apihub-service/resources/migrations/`
4. Add repository interface + implementation in `qubership-apihub-service/repository/`
5. Add service interface + implementation in `qubership-apihub-service/service/`
6. Add controller interface + implementation in `qubership-apihub-service/controller/`
7. Register route in `qubership-apihub-service/Service.go`

## Patterns
- Controller: parse request → check permissions → call service → respond
- Service: validate → business logic → call repository → return view
- Repository: go-pg query → return entity
- Use `exception.CustomError` for errors
- Use `context.SecurityContext` for auth context
