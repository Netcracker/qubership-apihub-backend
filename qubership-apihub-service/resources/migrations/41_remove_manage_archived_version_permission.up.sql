-- Issue #762: the 'archived' package version status is removed from the product.
-- Strip the now-unknown 'manage_archived_version' permission from every role that still
-- carries it: the seeded admin/owner/editor roles as well as any custom roles created
-- by users. Roles that never had the permission are left untouched by the WHERE clause.
UPDATE public.role
SET permissions = array_remove(permissions, 'manage_archived_version')
WHERE 'manage_archived_version' = ANY(permissions);

-- Live versions that still have status 'archived' would otherwise stay visible after the
-- status is removed from the API. Soft-delete them and stamp metadata.archived so the
-- down-migration can restore them. Versions that are already deleted are left as-is.
UPDATE public.published_version
SET
    deleted_at = now(),
    metadata = COALESCE(metadata, '{}'::jsonb) || '{"archived": true}'::jsonb
WHERE status = 'archived'
  AND deleted_at IS NULL;
