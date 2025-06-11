ALTER TABLE versions_cleanup_run
ALTER COLUMN package_id DROP NOT NULL;

ALTER TABLE versions_cleanup_run
ADD COLUMN instance_id uuid;