# New SQL Migration

Create a new PostgreSQL migration for qubership-apihub-backend.

## Rules
- Place files in `qubership-apihub-service/resources/migrations/`
- Check the latest migration number and use the next sequential number
- Create BOTH files: `{N}_{description}.up.sql` and `{N}_{description}.down.sql`
- Down migration MUST fully reverse the up migration
- Use `IF EXISTS` / `IF NOT EXISTS` for safety
- Never modify existing migrations that have been deployed

## Naming Examples
```
29_add_user_preferences.up.sql
29_add_user_preferences.down.sql
```
