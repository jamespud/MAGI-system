-- Issue #13: persist the authoritative structured minority record alongside the
-- resolution so the report, HTTP/A2A and export projections all read one
-- source instead of re-deriving dissent from the vote tables.
--
-- The column is nullable on purpose. NULL means the minority record was never
-- persisted (a resolution written before this migration), which is a different
-- fact from an explicit "no dissent". New resolutions always write their record
-- as JSON: "[]" (confirmed no dissent) or "[...]" (minority positions present).
-- Reading a legacy row therefore yields "not recorded", never a fabricated
-- consensus that nobody dissented.
ALTER TABLE resolution
    ADD COLUMN dissent_json TEXT NULL;
