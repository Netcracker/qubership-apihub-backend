---
description: Review selected code for adherence to project conventions
---

Review the selected code (or the file I'm pointing at) against this project's conventions and patterns. Check for:

## Architecture
- [ ] Controller does NOT contain business logic (only HTTP handling + delegation)
- [ ] Controller does NOT directly access repository layer
- [ ] Service contains all business logic
- [ ] Repository contains only data access code
- [ ] Correct layer separation: Controller → Service → Repository → Entity

## Error Handling
- [ ] All errors are handled (no ignored `err` returns)
- [ ] Business errors use `exception.CustomError{Status, Code, Message, Params}`
- [ ] Error codes are defined as constants in `exception/ErrorCodes.go`
- [ ] Controllers use `utils.RespondWithCustomError()` or `utils.RespondWithError()`

## Patterns
- [ ] Interface + private impl + `New*` constructor pattern
- [ ] Dependencies injected via constructor (interfaces, not concrete types)
- [ ] `context.SecurityContext` used (not stdlib context directly)
- [ ] Required params as separate args, optional via struct

## Security
- [ ] Mutating endpoints check permissions via `roleService.HasRequiredPermissions()`
- [ ] No secrets hardcoded in code

## Logging
- [ ] Write operations (POST/PUT/DELETE) logged at INFO
- [ ] Errors logged at ERROR
- [ ] No excessive GET logging at INFO level
- [ ] Async operations: start logged with ID, prefixed logs, completion logged

## API
- [ ] No breaking changes to existing endpoints without version bump
- [ ] Deprecated methods have `_deprecated` suffix

Report violations with specific line references and suggested fixes.
