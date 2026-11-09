-- Declared cost per package-unit of a lane (ADR 0014): the planner's declared
-- marginal cost of moving ONE package-unit across the lane on one departure,
-- integer cents. 0 is the default and means no declaration yet; legacy rows
-- keep it and read as 0.
ALTER TABLE lanes
    ADD COLUMN cost_per_unit_cents BIGINT NOT NULL DEFAULT 0
    CHECK (cost_per_unit_cents >= 0 AND cost_per_unit_cents <= 10000000);
