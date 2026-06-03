# Backend-local agent packages

Repository-specific APM packages for `qubership-apihub-backend`. Generic packages come from
[`qubership-apihub-ci/agent-packages`](https://github.com/Netcracker/qubership-apihub-ci/tree/apm_migration/agent-packages);
this folder holds content that applies **only** to this service.

## Packages

| Package | Path | Depends on (CI store) |
|---------|------|------------------------|
| `apihub-backend-developer` | `skills/apihub-backend-developer/` | `apihub-go-developer` |
| `apihub-backend-self-review` | `skills/apihub-backend-self-review/` | `apihub-go-self-review` |
| `backend-conventions` | `instructions/backend-conventions/` | — |

Root `apm.yml` lists both CI store dependencies and these local paths. After edits, run from
repository root:

```bash
apm install --target cursor,claude --legacy-skill-paths
```

Sources in `agent-packages/` and deployed `.cursor/` / `.claude/` harness trees are **committed**.
Only `apm_modules/` is gitignored.
