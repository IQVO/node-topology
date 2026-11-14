-- Optional declared monthly inbound volume of a profile revision (ADR 0019):
-- the cubic decimetres of inbound goods the node accepts per calendar
-- month. NULL = no monthly volume declared, which is what every existing
-- revision keeps. First-class column (own range check) beside the JSONB
-- inbound document of migration 0003.
ALTER TABLE node_profile_revisions
    ADD COLUMN monthly_volume_dm3 INTEGER NULL
    CHECK (monthly_volume_dm3 BETWEEN 1 AND 2147483647);
