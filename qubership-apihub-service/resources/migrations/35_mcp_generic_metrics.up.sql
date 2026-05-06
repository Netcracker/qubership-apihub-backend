WITH metric_map(old_metric, new_metric) AS (
    VALUES
        ('mcp_search_rest_operations_tool_called', 'mcp_search_operations_tool_called'),
        ('mcp_get_rest_operation_spec_tool_called', 'mcp_get_operation_spec_tool_called'),
        ('mcp_get_rest_operation_diff_tool_called', 'mcp_get_operation_diff_tool_called')
),
expanded AS (
    SELECT
        b.year,
        b.month,
        b.day,
        mm.new_metric AS metric,
        'rest|' || e.key AS key,
        sum(e.value::int) AS value,
        b.user_id
    FROM business_metric b
    JOIN metric_map mm ON b.metric = mm.old_metric
    CROSS JOIN LATERAL jsonb_each_text(coalesce(b.data, '{}'::jsonb)) e(key, value)
    GROUP BY b.year, b.month, b.day, mm.new_metric, 'rest|' || e.key, b.user_id
),
aggregated AS (
    SELECT
        year,
        month,
        day,
        metric,
        jsonb_object_agg(key, value) AS data,
        user_id
    FROM expanded
    GROUP BY year, month, day, metric, user_id
)
INSERT INTO business_metric (year, month, day, metric, data, user_id)
SELECT year, month, day, metric, data, user_id
FROM aggregated
ON CONFLICT (year, month, day, metric, user_id)
DO UPDATE
SET data = coalesce(business_metric.data, '{}'::jsonb) || (
    SELECT jsonb_object_agg(
        key,
        coalesce((business_metric.data ->> key)::int, 0) + value::int
    )
    FROM jsonb_each_text(EXCLUDED.data) e(key, value)
);

DELETE FROM business_metric
WHERE metric IN (
    'mcp_search_rest_operations_tool_called',
    'mcp_get_rest_operation_spec_tool_called',
    'mcp_get_rest_operation_diff_tool_called'
);
