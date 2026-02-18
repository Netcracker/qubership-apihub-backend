---
description: Explain how a feature works across all architecture layers
---

Trace the specified feature or endpoint through all architecture layers and explain how it works.

## What to show

For the feature/endpoint the user asks about, find and explain:

1. **Route** — how the HTTP route is registered in `Service.go` (path, method, security wrapper)
2. **Controller** — how the request is parsed, what permissions are checked, which service method is called
3. **Service** — what business logic is applied, what repositories are called, what validation happens
4. **Repository** — what database queries are executed, what entities are returned
5. **Entity** — what the database model looks like (table, columns)
6. **View** — what the API request/response DTOs look like

## Format

Show the actual code from each layer with brief explanations of the flow. Trace the data path from HTTP request to database and back.

## Reference

- @qubership-apihub-service/Service.go (route definitions start around line 330)
- @qubership-apihub-service/controller/ (HTTP handlers)
- @qubership-apihub-service/service/ (business logic)
- @qubership-apihub-service/repository/ (data access)
