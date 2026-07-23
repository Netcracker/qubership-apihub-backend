-- Best-effort rollback: restore exclude_from_search for personal private workspaces
-- (user_data.private_package_id) and remove their FTS rows.
-- Non-personal workspaces flipped by the up migration are not restored.

CREATE TEMP TABLE tmp_personal_private_ws ON COMMIT DROP AS
SELECT DISTINCT private_package_id AS workspace_id
FROM user_data
WHERE private_package_id IS NOT NULL
  AND private_package_id <> '';

CREATE TEMP TABLE tmp_personal_private_pkgs ON COMMIT DROP AS
SELECT pg.id AS package_id
FROM package_group pg
         INNER JOIN tmp_personal_private_ws tw
                    ON pg.id = tw.workspace_id
                        OR pg.id LIKE tw.workspace_id || '.%';

DELETE FROM fts_operation_search_text
WHERE package_id IN (SELECT package_id FROM tmp_personal_private_pkgs);

DELETE FROM fts_ddl_search_text
WHERE package_id IN (SELECT package_id FROM tmp_personal_private_pkgs);

DELETE FROM fts_mcp_search_text
WHERE package_id IN (SELECT package_id FROM tmp_personal_private_pkgs);

DELETE FROM global_search.fts_operation_search_text
WHERE package_id IN (SELECT package_id FROM tmp_personal_private_pkgs);

DELETE FROM global_search.fts_ddl_search_text
WHERE package_id IN (SELECT package_id FROM tmp_personal_private_pkgs);

DELETE FROM global_search.fts_mcp_search_text
WHERE package_id IN (SELECT package_id FROM tmp_personal_private_pkgs);

UPDATE package_group pg
SET exclude_from_search = true
FROM tmp_personal_private_ws tw
WHERE pg.id = tw.workspace_id
   OR pg.id LIKE tw.workspace_id || '.%';
