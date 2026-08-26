-- Issue #762: the 'archived' package version status is removed from the product.
-- Strip the now-unknown 'manage_archived_version' permission from every role that still
-- carries it: the seeded admin/owner/editor roles as well as any custom roles created
-- by users. Roles that never had the permission are left untouched by the WHERE clause.
UPDATE public.role
SET permissions = array_remove(permissions, 'manage_archived_version')
WHERE 'manage_archived_version' = ANY(permissions);
