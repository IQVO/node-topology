-- Optional inbound limits of a profile revision (ADR 0007): a JSONB object
-- {maxCartonWeightG, maxCartonDimsMm{l,w,h}, acceptsPallets, requiresSscc}
-- holding only the declared limits. NULL = no inbound limits declared, which
-- is what every existing revision keeps. The append-only trigger guards rows,
-- not the table shape.
ALTER TABLE node_profile_revisions
    ADD COLUMN inbound JSONB CHECK (inbound IS NULL OR jsonb_typeof(inbound) = 'object');
