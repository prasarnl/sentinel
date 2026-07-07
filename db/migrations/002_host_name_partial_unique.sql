-- A removed host shouldn't permanently reserve its name: only active
-- (non-removed) hosts need to be unique, so a name can be reused after
-- the old host is deleted.
ALTER TABLE hosts DROP CONSTRAINT hosts_name_key;
CREATE UNIQUE INDEX hosts_name_unique_active ON hosts (name) WHERE status != 'removed';
