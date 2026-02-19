---
description: Create a new REST API endpoint with all layers (controller, service, repository, entity, view, migration, route)
---

Create a new REST API endpoint following the project's layered architecture.

## What I need

The user will describe the endpoint they want. Based on their description, generate ALL required layers:

## Steps to follow

1. **View** (`qubership-apihub-service/view/`): Create request/response DTO structs with `json` tags. Follow existing naming patterns in the view package.

2. **Entity** (`qubership-apihub-service/entity/`): If the endpoint needs new DB tables/columns, create entity structs with `pg` struct tags matching the migration schema.

3. **SQL Migration** (`qubership-apihub-service/resources/migrations/`): If DB changes needed, create both `{N}_{description}.up.sql` and `{N}_{description}.down.sql`. Check existing migrations for the next sequential number. Down must fully reverse up.

4. **Repository** (`qubership-apihub-service/repository/`): Create interface + private impl + `New*` constructor. Use `db.ConnectionProvider` for database access via go-pg v10.

5. **Service** (`qubership-apihub-service/service/`): Create interface + private impl + `New*` constructor. All business logic here. Use `exception.CustomError` for errors. Use `context.SecurityContext` for auth context.

6. **Controller** (`qubership-apihub-service/controller/`): Create interface + private impl + `New*` constructor. Parse request → check permissions with `roleService.HasRequiredPermissions()` → call service → respond with `utils.RespondWithJson()` or `utils.RespondWithCustomError()`.

7. **Route Registration** in `qubership-apihub-service/Service.go`: Add `r.HandleFunc()` with the path, `security.Secure()` wrapper, and HTTP method. Wire all new dependencies in the main function.

8. **Error Codes** (`qubership-apihub-service/exception/ErrorCodes.go`): Add any new error code constants + message constants following existing patterns (code as string number, message with `$param` placeholders).

## Reference files

- @qubership-apihub-service/controller/PackageController.go (controller pattern)
- @qubership-apihub-service/service/PackageService.go (service pattern)
- @qubership-apihub-service/exception/ErrorCodes.go (error codes pattern)
- @qubership-apihub-service/exception/Errors.go (error types)
- @qubership-apihub-service/Service.go (route registration and dependency wiring)
