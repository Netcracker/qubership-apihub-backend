-- Migration 38: Trigger soft migration for fixing dashboard version comparison refs
-- The actual work is performed asynchronously by SoftMigrateDb function
-- This migration ensures the soft migration runs once when upgrading from version 37 to 38
-- No schema changes required, but PostgreSQL requires at least one statement
DO $$ BEGIN END $$;
