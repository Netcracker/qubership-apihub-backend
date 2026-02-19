---
description: Add a new scheduled cleanup job
---

Create a new scheduled cleanup job following the existing cleanup pattern in the project.

## Architecture

Cleanup jobs in this project follow this pattern:

1. **Repository** (`repository/`): handles the actual database cleanup queries
2. **Service** (`service/cleanup/`): manages the cron job scheduling and execution
3. **Configuration** (`config.template.yaml`): schedule (cron), timeout, TTL settings
4. **Wiring** (`Service.go`): creates the repository, registers the cleanup job

## Steps

1. Create a cleanup repository in `qubership-apihub-service/repository/` with the cleanup query logic.
2. Add a `Create{Name}CleanupJob()` method to the cleanup service in `qubership-apihub-service/service/cleanup/`.
3. Add configuration parameters under the `cleanup:` section of `config.template.yaml`.
4. Add config reading methods to `SystemInfoService`.
5. Wire everything in `Service.go` — create the repository and call `cleanupService.Create{Name}CleanupJob(...)`.
6. Use `lockService` for distributed locking to prevent concurrent execution across instances.

## Reference

- @qubership-apihub-service/service/cleanup/ (cleanup service)
- @qubership-apihub-service/repository/SoftDeletedDataCleanupRepository.go (cleanup repo example)
- @qubership-apihub-service/repository/ComparisonCleanupRepository.go (another example)
- @qubership-apihub-service/Service.go (job registration, around lines 222-234)
- @qubership-apihub-service/config.template.yaml (cleanup config section)
