alter table migration_run
    add instance_id varchar;

alter table migration_run
    add sequence_number serial not null;

alter table migration_run
    add constraint migration_run_seq
        unique (sequence_number);

alter table migration_run
    add post_check_result jsonb;

drop table if exists public.migrated_version;

