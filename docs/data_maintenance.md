# Data Maintenance

This document describes various data maintenance features available in the APIHUB backend.

## Revisions TTL

APIHUB backend implements an automatic cleanup mechanism for old package revisions to reduce migration size. The system runs a scheduled job that removes revisions older than a configured time-to-live (TTL) period and meet the configured conditions.

### Configuration

The revisions cleanup job is configured through environment variables:

| Environment Variable | Default Value | Description |
|---------------------|---------------|-------------|
| `REVISIONS_TTL_DAYS` | `365` (1 year) | Number of days to keep revisions before they become eligible for deletion |
| `REVISIONS_CLEANUP_DELETE_LAST_REVISION` | `false` | Whether to delete the last revision of a version even if it's the only one |
| `REVISIONS_CLEANUP_DELETE_RELEASE_REVISIONS` | `false` | Whether to delete revisions with "release" status |
| `REVISIONS_CLEANUP_SCHEDULE` | `0 21 * * 0` | Cron schedule for the cleanup job (Sunday 9:00 PM by default) |

The job timeout is automatically calculated based on the schedule interval to ensure it completes before the next run.

### How job works

The revisions cleanup job performs the following steps:

1. Checks if any migrations are running - if so, it skips execution to avoid conflicts.
2. Processes packages in batches, examining each version's revisions.
3. For each revision, checks:
   - If it's older than the TTL value
   - If it's the last revision (and whether deletion of last revisions is enabled)
   - If it has "release" status (and whether deletion of release revisions is enabled)
   - If it has any valid references from dashboards
4. Deletes eligible revisions and tracks the deletion in the activity log.
5. Cleans up related data like ad-hoc comparisons, default release version and previous version for other versions (in case of version deletion).

### Deletion modes

The cleanup job has different modes that can be configured:

#### Standard mode
By default (`REVISIONS_CLEANUP_DELETE_LAST_REVISION=false` and `REVISIONS_CLEANUP_DELETE_RELEASE_REVISIONS=false`), the job will delete revisions that are:
- Older than the configured TTL (default: 365 days)
- Not the last revision of a version
- Not in "release" status
- Not referenced by dashboards

#### Last Revision Cleanup mode
When `REVISIONS_CLEANUP_DELETE_LAST_REVISION=true` and `REVISIONS_CLEANUP_DELETE_RELEASE_REVISIONS=false`:
- Deletes revisions older than the TTL, including the last revision of a version
- Preserves revisions with "release" status
- Can completely remove versions if all revisions meet deletion criteria and none are releases
- Never deletes revisions referenced by dashboards

#### Release Revisions Cleanup mode
When `REVISIONS_CLEANUP_DELETE_LAST_REVISION=false` and `REVISIONS_CLEANUP_DELETE_RELEASE_REVISIONS=true`:
- Deletes revisions older than the TTL, including those with "release" status
- Preserves the last revision of each version
- Never deletes revisions referenced by dashboards

This should be used with caution as release revisions often represent important milestones.

#### Delete All mode
When both `REVISIONS_CLEANUP_DELETE_LAST_REVISION=true` and `REVISIONS_CLEANUP_DELETE_RELEASE_REVISIONS=true`:
- Deletes any revision older than the TTL regardless of whether it's the last revision or has "release" status
- Can completely remove versions if all revisions meet deletion criteria
- Never deletes revisions referenced by dashboards

> **WARNING**: Delete All mode is not recommended for use with short TTL and should only be used with recent database backups. The job in this mode can potentially delete all published versions in the system if the TTL is too short.

## Ad-hoc comparisons TTL

APIHUB backend implements an automatic cleanup mechanism for version/operation comparisons to reduce database size and migration size. The system runs a scheduled job that removes old and irrelevant comparisons, primarily focusing on "ad-hoc" comparisons that are created for temporary analysis.

### Configuration

The comparisons cleanup job is configured through environment variables:

| Environment Variable | Default Value | Description |
|---------------------|---------------|-------------|
| `COMPARISONS_TTL_DAYS` | `30` | Number of days to keep ad-hoc comparisons before they become eligible for deletion |
| `COMPARISONS_CLEANUP_SCHEDULE` | `0 5 * * 0` | Cron schedule for the cleanup job (Sunday 5:00 AM by default) |
| `COMPARISONS_CLEANUP_TIMEOUT_MIN` | `720` (12 hours) | Maximum execution time for the cleanup job in minutes |

The job includes a vacuum phase that runs after the main cleanup to optimize affected database tables.

### How job works

The comparisons cleanup job performs the following steps:

1. Checks if any migrations are running - if so, it skips execution to avoid conflicts.
2. Iterates through all version comparisons in the system.
3. For each comparison, it checks for several deletion criteria. A comparison is deleted if any of the following are true:
   - It is an ad-hoc comparison older than the configured TTL. An ad-hoc comparison is one that was created between two arbitrary versions, not as part of a version's changelog.
   - It is an outdated changelog comparison, meaning it does not point to the latest revision of the previous version.
   - It is a comparison for a revision that no longer exists.
4. Deletes eligible version comparisons and related operation comparisons.
5. Performs VACUUM FULL on affected comparison tables to optimize database size.

## Soft Deleted Data TTL

APIHUB backend implements an automatic cleanup mechanism for soft-deleted data to permanently remove data that has been previously marked for deletion. The system runs a scheduled job that removes soft-deleted data older than a configured time-to-live (TTL) period.

### Configuration

The soft deleted data cleanup job is configured through environment variables:

| Environment Variable | Default Value | Description |
|---------------------|---------------|-------------|
| `SOFT_DELETED_DATA_TTL_DAYS` | `730` (2 years) | Number of days to keep soft-deleted data before permanent deletion |
| `SOFT_DELETED_DATA_CLEANUP_SCHEDULE` | `0 22 * * 5` | Cron schedule for the cleanup job (Friday 10:00 PM by default) |
| `SOFT_DELETED_DATA_CLEANUP_TIMEOUT_MIN` | `1440` (24 hours) | Maximum execution time for the cleanup job in minutes |

### How job works

The soft deleted data cleanup job performs the following steps:

1. Checks if any migrations are running - if so, it skips execution to avoid conflicts.
2. Processes soft-deleted packages in batches, permanently deleting those older than the TTL.
3. Processes soft-deleted package revisions in batches, permanently deleting those older than the TTL.
4. Performs VACUUM FULL on affected database tables to reclaim disk space and optimize performance.

### Affected Tables and Handling

The cleanup job affects the following database tables:

- **package_group** - package entities
- **published_version** - package revisions
- **activity_tracking** – related to `package_group`, but linked to a revision; however, these should not be deleted when a revision is deleted. Only remove records when deleting a package through cascade deletion.
- **apihub_api_keys** – remove the API keys that were issued for the packages being removed
- **build** – related to `package_group`, all related records are automatically removed through cascade deletion
- **build_depends** – related to `build`, all related records are automatically removed through cascade deletion
- **build_result** – related to `build`, all related records are automatically removed through cascade deletion
- **build_src** – related to `build`, all related records are automatically removed through cascade deletion
- **builder_notifications** – related to `build`, all related records are automatically removed through cascade deletion
- **favorite_packages** – related to `package_group`, all related records are automatically removed through cascade deletion
- **migrated_version** – related to `package_group`, all related records are automatically removed through cascade deletion
- **operation** – related to `published_version`, all related records are automatically removed through cascade deletion
- **operation_group** – related to `published_version`, all related records are automatically removed through cascade deletion
- **grouped_operation** – related to `operation` and `operation_group` (many-to-many relationship), all related records are automatically removed through cascade deletion
- **operation_group_history** – do not touch the records, as this table can be useful for analysis
- **operation_open_count** – related to `package_group`, all related records are automatically removed through cascade deletion
- **package_export_config** – related to `package_group`, all related records are automatically removed through cascade deletion
- **package_member_role** – related to `package_group`, all related records are automatically removed through cascade deletion
- **package_service** – related to `package_group`, all related records are automatically removed through cascade deletion
- **package_transition** – has no relations, remove records that contain the packages being removed in the `new_package_id` column
- **published_document_open_count** – related to `package_group`, all related records are automatically removed through cascade deletion
- **published_sources** – related to `published_version`, all related records are automatically removed through cascade deletion
- **published_version_open_count** – related to `package_group`, all related records are automatically removed through cascade deletion
- **published_version_reference** – related to `published_version`, all related records are automatically removed through cascade deletion
- **published_version_revision_content** – related to `published_version` and `published_data`, all related records are automatically removed through cascade deletion
- **published_version_validation** – related to `published_version`, all related records are automatically removed through cascade deletion
- **shared_url_info** – related to `package_group`, all related records are automatically removed through cascade deletion
- **transformed_content_data** – related to `operation_group`, all related records are automatically removed through cascade deletion

## Cleanup Job Schedules

All cleanup jobs run on predefined schedules to avoid conflicts and distribute system load:

| Job Type | Default Schedule | Description | Day/Time |
|----------|------------------|-------------|----------|
| Revisions Cleanup | `0 21 * * 0` | Sunday at 9:00 PM | Every Sunday |
| Comparisons Cleanup | `0 5 * * 0` | Sunday at 5:00 AM | Every Sunday |
| Soft Deleted Data Cleanup | `0 22 * * 5` | Friday at 10:00 PM | Every Friday |
| Builds Cleanup | `0 1 * * 0` | Sunday at 1:00 AM | Every Sunday |

All schedules use UTC timezone and can be customized via environment variables.
