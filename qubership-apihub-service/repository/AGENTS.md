# AGENTS.md — Repository Layer

Data access layer using go-pg v10 ORM for PostgreSQL.

## Pattern
```go
type FooRepository interface {
    GetById(id string) (*entity.FooEntity, error)
}
type fooRepositoryImpl struct {
    cp db.ConnectionProvider
}
func NewFooRepository(cp db.ConnectionProvider) FooRepository {
    return &fooRepositoryImpl{cp: cp}
}
```

## Rules
- NO business logic — only database queries
- Returns entity types (not views)
- Connection via `r.cp.GetConnection()`
- Use go-pg query builder: `.Model(&entity).Where("col = ?", val).Select()`
- Always propagate errors
