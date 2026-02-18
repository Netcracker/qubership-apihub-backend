# AGENTS.md — Controller Layer

HTTP request handlers. Each controller is an interface + private implementation.

## Responsibilities
- Parse HTTP request (path params, query params, body)
- Create SecurityContext: `ctx := context.Create(r)`
- Check permissions: `roleService.HasRequiredPermissions(ctx, resourceId, permission)`
- Delegate to service layer
- Return response: `utils.RespondWithJson(w, status, data)` or `utils.RespondWithCustomError(w, err)`

## Rules
- NO business logic — only HTTP plumbing
- NO direct repository access
- ALWAYS check permissions for mutating operations
- Use `handlePkgRedirectOrRespondWithError` for package-related errors (handles transitions)
- Path params: `getStringParam(r, "name")`
- Query params: `r.URL.Query().Get("name")`
