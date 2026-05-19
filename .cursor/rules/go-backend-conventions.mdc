---
description: Go backend conventions for qubership-apihub-service
globs: qubership-apihub-service/**/*.go
alwaysApply: false
---

# Go Backend Conventions

## Constants and literals

- Do not use magic numbers; declare named constants.
- If a numeric literal is unavoidable, add a brief comment explaining what it is and why that value is used.
- If a string literal is repeated, extract it to a constant.

## HTTP status codes

- Use `net/http` named constants (`http.StatusOK`, `http.StatusBadRequest`, `http.StatusNotFound`, etc.).
- Do not use raw status integers (e.g. `200`, `400`, `404`) in handlers, middleware, or tests.

## Comments

- Comment only when it materially helps understanding non-obvious logic.
- Do not comment obvious code.
- Do not add comments that map structs/functions to HTTP routes (e.g. `// AiChatsListResponse is GET /chats`).

## Entity → view converters

- Converters with **no dependencies** belong in the `entity` package next to the entity struct.
- Name them `Make{Name}View` (e.g. `MakePackageSearchResultView`).

## Service.go wiring

- Add new repositories, services, and controllers at the **end** of their corresponding sections in `Service.go`.
- Use `log.Fatalf` for fail-fast fatal errors during wiring/startup in `Service.go` when initialization cannot continue.

## API errors

- Error codes and messages returned in HTTP responses must be constants in `exception/ErrorCodes.go`.
