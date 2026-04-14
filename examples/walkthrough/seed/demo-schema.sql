--------------------------------------------------------------------------
--
-- pgEdge AI DBA Workbench
--
-- Copyright (c) 2025 - 2026, pgEdge, Inc.
-- This software is released under The PostgreSQL License
--
--------------------------------------------------------------------------
-- =======================================================================
-- Production E-Commerce Database - Full Seed
-- =======================================================================
-- Target: pgEdge PostgreSQL 18 with Spock (demo-pg-prod-primary)
-- Database: ecommerce
--
-- This script initializes the production e-commerce database with
-- schema, indexes, and realistic seed data. It deliberately omits
-- certain indexes and leaves table bloat to create scenarios the
-- AI DBA Workbench can detect and recommend fixes for:
--
--   - Missing index on orders(customer_id)
--   - Missing index on sessions(expires_at)
--   - Table bloat on sessions (160K dead tuples from bulk delete)
--   - Stale statistics on sessions (ANALYZE deliberately skipped)
--
-- The docker-compose configuration disables autovacuum on this
-- container so the bloat conditions persist for demonstration.
-- =======================================================================

-- -----------------------------------------------------------------------
-- Extensions
-- -----------------------------------------------------------------------

CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
CREATE EXTENSION IF NOT EXISTS system_stats;
CREATE EXTENSION IF NOT EXISTS vector;

-- -----------------------------------------------------------------------
-- Tables
-- -----------------------------------------------------------------------

CREATE TABLE customers (
    id         SERIAL PRIMARY KEY,
    name       TEXT NOT NULL,
    email      TEXT NOT NULL,
    region     TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE customers IS
    'Registered e-commerce customers across four geographic regions';

CREATE TABLE products (
    id         SERIAL PRIMARY KEY,
    name       TEXT NOT NULL,
    category   TEXT NOT NULL,
    price      NUMERIC(10, 2) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE products IS
    'Product catalog with category classification and pricing';

CREATE TABLE orders (
    id           SERIAL PRIMARY KEY,
    customer_id  INTEGER NOT NULL REFERENCES customers (id),
    order_date   TIMESTAMPTZ NOT NULL DEFAULT now(),
    status       TEXT NOT NULL,
    total_amount NUMERIC(12, 2) NOT NULL
);

COMMENT ON TABLE orders IS
    'Customer orders with status tracking and total amounts';

CREATE TABLE order_items (
    id         SERIAL PRIMARY KEY,
    order_id   INTEGER NOT NULL REFERENCES orders (id),
    product_id INTEGER NOT NULL REFERENCES products (id),
    quantity   INTEGER NOT NULL,
    unit_price NUMERIC(10, 2) NOT NULL
);

COMMENT ON TABLE order_items IS
    'Individual line items within each order';

CREATE TABLE inventory (
    id           SERIAL PRIMARY KEY,
    product_id   INTEGER NOT NULL REFERENCES products (id),
    warehouse    TEXT NOT NULL,
    quantity     INTEGER NOT NULL,
    last_updated TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE inventory IS
    'Current stock levels per product across warehouse locations';

CREATE TABLE sessions (
    id         SERIAL PRIMARY KEY,
    user_id    INTEGER,
    token      TEXT,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    data       JSONB
);

COMMENT ON TABLE sessions IS
    'Active and expired user sessions with token data';

-- -----------------------------------------------------------------------
-- Indexes
-- -----------------------------------------------------------------------

-- Customers
CREATE INDEX idx_customers_email  ON customers (email);
CREATE INDEX idx_customers_region ON customers (region);

-- Products
CREATE INDEX idx_products_category ON products (category);

-- Orders
-- NOTE: index on orders(customer_id) is DELIBERATELY OMITTED.
-- This creates a missing-index scenario for the workbench to detect.
CREATE INDEX idx_orders_status     ON orders (status);
CREATE INDEX idx_orders_order_date ON orders (order_date);

-- Order items
CREATE INDEX idx_order_items_order_id   ON order_items (order_id);
CREATE INDEX idx_order_items_product_id ON order_items (product_id);

-- Inventory
CREATE INDEX idx_inventory_product_id ON inventory (product_id);

-- Sessions
-- NOTE: index on sessions(expires_at) is DELIBERATELY OMITTED.
-- This creates another missing-index scenario for the workbench.

-- -----------------------------------------------------------------------
-- Seed Data
-- -----------------------------------------------------------------------

-- Customers: 100,000 rows across four regions
INSERT INTO customers (name, email, region, created_at)
SELECT
    'Customer ' || gs.id,
    'customer' || gs.id || '@example.com',
    (ARRAY['north', 'south', 'east', 'west'])[1 + (gs.id % 4)],
    now() - (random() * INTERVAL '730 days')
FROM generate_series(1, 100000) AS gs(id);

-- Products: 5,000 rows across eight categories
INSERT INTO products (name, category, price, created_at)
SELECT
    'Product ' || gs.id,
    (ARRAY[
        'Electronics', 'Clothing', 'Home & Garden', 'Sports',
        'Books', 'Toys', 'Food & Beverage', 'Health & Beauty'
    ])[1 + (gs.id % 8)],
    round((random() * 499 + 1)::NUMERIC, 2),
    now() - (random() * INTERVAL '365 days')
FROM generate_series(1, 5000) AS gs(id);

-- Orders: 500,000 rows with five statuses
INSERT INTO orders (customer_id, order_date, status, total_amount)
SELECT
    1 + (gs.id % 100000),
    now() - (random() * INTERVAL '365 days'),
    (ARRAY[
        'pending', 'processing', 'shipped', 'delivered', 'cancelled'
    ])[1 + (gs.id % 5)],
    round((random() * 999 + 1)::NUMERIC, 2)
FROM generate_series(1, 500000) AS gs(id);

-- Order items: ~1,200,000 rows (two to three items per order on average)
INSERT INTO order_items (order_id, product_id, quantity, unit_price)
SELECT
    1 + (gs.id % 500000),
    1 + (gs.id % 5000),
    1 + (gs.id % 10),
    round((random() * 199 + 1)::NUMERIC, 2)
FROM generate_series(1, 1200000) AS gs(id);

-- Inventory: 10,000 rows across five warehouses
INSERT INTO inventory (product_id, warehouse, quantity, last_updated)
SELECT
    1 + (gs.id % 5000),
    (ARRAY[
        'warehouse-east', 'warehouse-west', 'warehouse-central',
        'warehouse-north', 'warehouse-south'
    ])[1 + (gs.id % 5)],
    (random() * 500)::INTEGER,
    now() - (random() * INTERVAL '30 days')
FROM generate_series(1, 10000) AS gs(id);

-- -----------------------------------------------------------------------
-- Bloat Simulation: Sessions Table
-- -----------------------------------------------------------------------
-- Insert 200K sessions then delete 80% to leave 160K dead tuples.
-- Autovacuum is disabled on this container so dead tuples persist.

INSERT INTO sessions (user_id, token, expires_at, created_at, data)
SELECT
    1 + (gs.id % 100000),
    md5(random()::TEXT),
    now() + (random() * INTERVAL '24 hours') - INTERVAL '12 hours',
    now() - (random() * INTERVAL '7 days'),
    jsonb_build_object(
        'ip', '10.' || (gs.id % 256) || '.' || ((gs.id / 256) % 256) || '.1',
        'agent', (ARRAY[
            'Mozilla/5.0', 'Chrome/120', 'Safari/17', 'Edge/120'
        ])[1 + (gs.id % 4)]
    )
FROM generate_series(1, 200000) AS gs(id);

-- Delete 80% of sessions, keeping only rows where id is divisible by 5.
-- This leaves approximately 40K live rows and 160K dead tuples.
DELETE FROM sessions WHERE id % 5 != 0;

-- -----------------------------------------------------------------------
-- Statistics
-- -----------------------------------------------------------------------
-- ANALYZE all tables EXCEPT sessions. Leaving sessions with stale
-- statistics creates a scenario the workbench can detect and advise on.

ANALYZE customers;
ANALYZE products;
ANALYZE orders;
ANALYZE order_items;
ANALYZE inventory;
