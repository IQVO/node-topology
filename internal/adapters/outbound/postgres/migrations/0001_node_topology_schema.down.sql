DROP TABLE IF EXISTS service_areas;
DROP TABLE IF EXISTS lanes;
ALTER TABLE IF EXISTS nodes DROP CONSTRAINT IF EXISTS nodes_current_profile_fkey;
DROP TABLE IF EXISTS node_profile_revisions;
DROP FUNCTION IF EXISTS node_profile_revisions_append_only();
DROP TABLE IF EXISTS nodes;
DROP TABLE IF EXISTS regions;
