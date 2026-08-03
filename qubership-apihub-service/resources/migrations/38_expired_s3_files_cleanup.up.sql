CREATE TABLE expired_s3_files_cleanup_run
(
    run_id        UUID PRIMARY KEY,
    instance_id   UUID                        NOT NULL,
    started_at    TIMESTAMP WITHOUT TIME ZONE NOT NULL DEFAULT NOW(),
    finished_at   TIMESTAMP WITHOUT TIME ZONE,
    status        VARCHAR                     NOT NULL,
    details       VARCHAR,
    deleted_items INTEGER
);
