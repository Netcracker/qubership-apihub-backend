WITH metric_map(new_metric, old_metric) AS (
    VALUES
        ('mcp_search_operations_tool_called', 'mcp_search_rest_operations_tool_called'),
        ('mcp_get_operation_spec_tool_called', 'mcp_get_rest_operation_spec_tool_called'),
        ('mcp_get_operation_diff_tool_called', 'mcp_get_rest_operation_diff_tool_called')
),
expanded AS (
    SELECT
        b.year,
        b.month,
        b.day,
        mm.old_metric AS metric,
        substring(e.key from 6) AS key,
        sum(e.value::int) AS value,
        b.user_id
    FROM business_metric b
    JOIN metric_map mm ON b.metric = mm.new_metric
    CROSS JOIN LATERAL jsonb_each_text(coalesce(b.data, '{}'::jsonb)) e(key, value)
    WHERE e.key LIKE 'rest|%'
    GROUP BY b.year, b.month, b.day, mm.old_metric, substring(e.key from 6), b.user_id
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

WITH metric_map(new_metric) AS (
    VALUES
        ('mcp_search_operations_tool_called'),
        ('mcp_get_operation_spec_tool_called'),
        ('mcp_get_operation_diff_tool_called')
)
UPDATE business_metric b
SET data = coalesce(
    (
        SELECT jsonb_object_agg(e.key, e.value)
        FROM jsonb_each(b.data) e(key, value)
        WHERE e.key NOT LIKE 'rest|%'
    ),
    '{}'::jsonb
)
FROM metric_map mm
WHERE b.metric = mm.new_metric;

DELETE FROM business_metric
WHERE metric IN (
    'mcp_search_operations_tool_called',
    'mcp_get_operation_spec_tool_called',
    'mcp_get_operation_diff_tool_called'
)
AND data = '{}'::jsonb;
