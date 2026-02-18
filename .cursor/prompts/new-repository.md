---
description: Create a new repository with interface, implementation, and constructor
---

Create a new repository component for database access following the project's patterns.

## Pattern to follow

```go
package repository

import "github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/db"

type {Name}Repository interface {
    // query methods here
}

type {name}RepositoryImpl struct {
    cp db.ConnectionProvider
}

func New{Name}Repository(cp db.ConnectionProvider) {Name}Repository {
    return &{name}RepositoryImpl{cp: cp}
}

// Method implementations using r.cp.GetConnection().Model(&entity)...
```

## Rules

1. Repository handles ONLY database queries — no business logic.
2. Returns entity types (from `entity/` package), never view types.
3. Uses `db.ConnectionProvider` to get database connection.
4. Uses go-pg v10 query builder: `.Model(&ent).Where("col = ?", val).Select()`.
5. Always propagate errors to the caller.
6. After creating, wire it in `qubership-apihub-service/Service.go`: `someRepo := repository.NewSomeRepository(cp)`.

## Reference

- @qubership-apihub-service/repository/BuildRepository.go (repository example)
- @qubership-apihub-service/entity/ (entity types)
- @qubership-apihub-service/db/ (connection provider)
