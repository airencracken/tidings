ALTER TABLE users ADD COLUMN suspended INTEGER NOT NULL DEFAULT 0 CHECK(suspended IN(0,1));
ALTER TABLE invites ADD COLUMN revoked INTEGER NOT NULL DEFAULT 0 CHECK(revoked IN(0,1));
ALTER TABLE entries ADD COLUMN import_owner INTEGER REFERENCES users(id) ON DELETE CASCADE;
ALTER TABLE entries ADD COLUMN original_key TEXT NOT NULL DEFAULT '';
CREATE INDEX entries_import_owner ON entries(import_owner);
