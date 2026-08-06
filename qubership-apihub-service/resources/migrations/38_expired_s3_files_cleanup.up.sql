CREATE TABLE expired_s3_files_cleanup_run
(
    run_id        UUID PRIMARY KEY,
    started_at    TIMESTAMP WITHOUT TIME ZONE NOT NULL DEFAULT NOW(),
    finished_at   TIMESTAMP WITHOUT TIME ZONE,
    details       VARCHAR,
    deleted_items INTEGER
);
