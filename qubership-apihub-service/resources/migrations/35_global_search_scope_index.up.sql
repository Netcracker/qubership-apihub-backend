CREATE INDEX IF NOT EXISTS fts_operation_search_text_scope_idx
    ON fts_operation_search_text (status, api_type, package_id varchar_pattern_ops);
