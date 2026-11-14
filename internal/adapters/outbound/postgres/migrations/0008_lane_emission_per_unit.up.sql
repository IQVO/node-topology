-- Declared carbon emission per package-unit of a lane (ADR 0018): the
-- planner's declared emission of moving ONE package-unit across the lane
-- on one departure, grams of CO2-equivalent. 0 is the default and means
-- no declaration yet; legacy rows keep it and read as 0.
ALTER TABLE lanes
    ADD COLUMN emission_per_unit_g BIGINT NOT NULL DEFAULT 0
    CHECK (emission_per_unit_g >= 0 AND emission_per_unit_g <= 10000000);
