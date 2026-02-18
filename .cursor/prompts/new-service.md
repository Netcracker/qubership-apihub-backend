---
description: Create a new service with interface, implementation, and constructor
---

Create a new service component following the project's patterns.

## Pattern to follow

```go
package service

type {Name}Service interface {
    // methods here
}

type {name}ServiceImpl struct {
    // dependencies as interfaces
}

func New{Name}Service(/* dependencies */) {Name}Service {
    return &{name}ServiceImpl{/* field assignments */}
}

// Method implementations on the impl struct
```

## Rules

1. Define a public interface with all methods.
2. Create a private (lowercase) implementation struct.
3. Constructor function `New{Name}Service()` returns the interface type.
4. Dependencies are injected via constructor (interfaces, not concrete types).
5. Use `exception.CustomError{Status, Code, Message, Params}` for business errors.
6. Use `context.SecurityContext` for auth context (project's own, not stdlib).
7. Log with logrus (`log`): INFO for write operations, ERROR for all errors.
8. After creating the service, wire it in `qubership-apihub-service/Service.go` main function.

## Reference

- @qubership-apihub-service/service/PackageService.go (canonical service example)
- @qubership-apihub-service/Service.go (dependency wiring)
