SELECT clock_timestamp(),
       count(*) FILTER (WHERE pid <> pg_backend_pid()),
       count(*) FILTER (WHERE pid <> pg_backend_pid() AND state = 'active'),
       count(*) FILTER (WHERE pid <> pg_backend_pid() AND wait_event_type = 'Lock'),
       count(*) FILTER (WHERE pid <> pg_backend_pid() AND state = 'idle in transaction'),
       COALESCE(round(extract(epoch FROM (max(clock_timestamp() - xact_start) FILTER (WHERE pid <> pg_backend_pid() AND xact_start IS NOT NULL))) * 1000), 0),
       (SELECT count(*) FROM pg_locks WHERE NOT granted),
       (SELECT deadlocks FROM pg_stat_database WHERE datname = current_database())
FROM pg_stat_activity
WHERE datname = current_database();
\watch 0.5
