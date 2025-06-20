create table grouped_operation_history
(
    group_id varchar                     not null,
    time     timestamp without time zone not null,
    reason   varchar                     not null,
    data     jsonb
);

