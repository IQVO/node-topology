-- The Idempotency-Key is global (REST guideline): a key is looked up by itself
-- and compared by request fingerprint (method + path + body hash), so the same
-- key on another route is a reuse (409), not a new key. Keys live 24 hours, so
-- the rows of the old (route, key) design are dropped rather than deduplicated.
DELETE FROM idempotency_keys;
ALTER TABLE idempotency_keys DROP CONSTRAINT idempotency_keys_pkey;
ALTER TABLE idempotency_keys ADD PRIMARY KEY (key);
