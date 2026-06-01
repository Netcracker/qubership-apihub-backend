# Backend-local agent skills and rules

Repository-specific agent packages for `qubership-apihub-backend`. Generic skills and
rules come from [`qubership-apihub-ci/agent-skills`](https://github.com/Netcracker/qubership-apihub-ci/tree/apm_migration/agent-skills);
this folder holds content that applies **only** to this service.

## Packages

| Package | Path | Depends on (CI store) |
|---------|------|------------------------|
| `apihub-backend-developer` | `skills/apihub-backend-developer/` | `apihub-go-developer` |
| `apihub-backend-self-review` | `skills/apihub-backend-self-review/` | `apihub-go-self-review` |
| `backend-conventions` | `instructions/backend-conventions/` | — |

Root `apm.yml` lists both CI store dependencies and these local paths. Run from repository root:

```bash
apm install --target cursor,claude --legacy-skill-paths
```

Sources here are **committed**; deployed `.cursor/` and `.claude/` trees remain gitignored.
