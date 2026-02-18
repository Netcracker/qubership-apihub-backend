---
description: Create a new controller with HTTP handlers
---

Create a new controller component for HTTP request handling following the project's patterns.

## Pattern to follow

```go
package controller

type {Name}Controller interface {
    Get{Thing}(w http.ResponseWriter, r *http.Request)
    Create{Thing}(w http.ResponseWriter, r *http.Request)
    // etc.
}

type {name}ControllerImpl struct {
    {name}Service service.{Name}Service
    roleService   service.RoleService
}

func New{Name}Controller(svc service.{Name}Service, roleService service.RoleService) {Name}Controller {
    return &{name}ControllerImpl{
        {name}Service: svc,
        roleService:   roleService,
    }
}
```

## Each handler method should

1. Extract path/query params: `getStringParam(r, "paramName")`, `r.URL.Query().Get("name")`
2. Create security context: `ctx := context.Create(r)`
3. Check permissions (for mutating ops): `roleService.HasRequiredPermissions(ctx, id, view.SomePermission)`
4. Parse request body if needed: `json.NewDecoder(r.Body).Decode(&req)`
5. Call service method
6. Respond: `utils.RespondWithJson(w, http.StatusOK, result)` or `utils.RespondWithCustomError(w, err)`

## After creating

Register routes in `qubership-apihub-service/Service.go`:
```go
r.HandleFunc("/api/v2/path/{param}", security.Secure(controller.MethodName)).Methods(http.MethodGet)
```

## Reference

- @qubership-apihub-service/controller/PackageController.go (controller pattern)
- @qubership-apihub-service/controller/OperationController.go (another example)
- @qubership-apihub-service/Service.go (route registration, lines 330+)
