alter table migration_run
    drop column instance_id;

alter table migration_run
    drop constraint migration_run_seq;

alter table migration_run
    drop column sequence_number;

