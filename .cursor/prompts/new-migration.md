---
description: Create a new SQL database migration (up + down)
---

Create a new PostgreSQL migration for the project.

## Rules

1. Check existing migrations in `qubership-apihub-service/resources/migrations/` to determine the next sequential number.
2. Create TWO files:
   - `{N}_{description}.up.sql` — the forward migration
   - `{N}_{description}.down.sql` — the rollback (must fully reverse the up migration)
3. Use `IF EXISTS` / `IF NOT EXISTS` for safety.
4. Description should be snake_case and descriptive.
5. If adding columns, also update the corresponding entity struct in `qubership-apihub-service/entity/`.

## Reference

- @qubership-apihub-service/resources/migrations/ (existing migrations for numbering and style)
- @qubership-apihub-service/entity/ (entity structs to update)
