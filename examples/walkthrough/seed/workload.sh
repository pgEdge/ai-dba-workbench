#!/bin/bash
#--------------------------------------------------------------------------
#
# pgEdge AI DBA Workbench
#
# Copyright (c) 2025 - 2026, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#--------------------------------------------------------------------------
# Production workload generator
# Generates: slow queries, lock contention, connection pressure,
#            session churn (bloat), and INSERT traffic.

set -e

echo "Waiting 30s for database initialization..."
sleep 30

echo "Starting production workload..."

run_query() {
    psql -q -c "$1" 2>/dev/null || true
}

while true; do
    # --- Slow unindexed JOIN (missing index on orders.customer_id) ---
    run_query "SELECT c.name, COUNT(o.id), SUM(o.total_amount)
               FROM orders o JOIN customers c ON o.customer_id = c.id
               WHERE c.region = 'west'
               GROUP BY c.name
               ORDER BY SUM(o.total_amount) DESC
               LIMIT 20;"

    # --- Sequential scan on sessions (no index on expires_at) ---
    run_query "SELECT COUNT(*) FROM sessions
               WHERE expires_at < now() - interval '7 days';"

    # --- Heavy aggregation ---
    run_query "SELECT p.category, DATE_TRUNC('month', o.order_date) AS month,
                      COUNT(*), SUM(oi.quantity * oi.unit_price) AS revenue
               FROM order_items oi
               JOIN orders o ON oi.order_id = o.id
               JOIN products p ON oi.product_id = p.id
               GROUP BY p.category, month
               ORDER BY month DESC, revenue DESC;"

    # --- Lock contention: concurrent inventory updates ---
    for j in $(seq 1 3); do
        run_query "UPDATE inventory SET quantity = quantity - 1,
                   last_updated = now()
                   WHERE product_id = $((RANDOM % 100 + 1))
                   AND warehouse = 'warehouse-east';" &
    done

    # --- New orders (INSERT traffic) ---
    run_query "INSERT INTO orders (customer_id, order_date, status, total_amount)
               SELECT (random() * 99999 + 1)::int, now(),
                      'pending', (random() * 200 + 10)::numeric(12,2)
               FROM generate_series(1, 50);"

    # --- Session churn (maintains bloat) ---
    run_query "DELETE FROM sessions
               WHERE id IN (SELECT id FROM sessions ORDER BY random() LIMIT 100);"
    run_query "INSERT INTO sessions (user_id, token, expires_at, data)
               SELECT (random() * 99999 + 1)::int, md5(random()::text),
                      now() + interval '1 hour', '{\"ip\": \"10.0.0.1\"}'
               FROM generate_series(1, 100);"

    wait
    sleep 2
done
