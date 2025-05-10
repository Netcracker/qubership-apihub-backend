create table export_result
(
    export_id character varying
        constraint export_result_pk
            primary key,
    config    json  not null,
    data      bytea not null
);
