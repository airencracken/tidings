ALTER TABLE users ADD COLUMN keyboard_shortcuts INTEGER NOT NULL DEFAULT 0 CHECK(keyboard_shortcuts IN(0,1));
ALTER TABLE entries ADD COLUMN body_html TEXT NOT NULL DEFAULT '';
ALTER TABLE feeds ADD COLUMN next_check INTEGER NOT NULL DEFAULT 0 CHECK(next_check>=0);
ALTER TABLE feeds ADD COLUMN poll_interval INTEGER NOT NULL DEFAULT 1800 CHECK(poll_interval BETWEEN 300 AND 86400);
ALTER TABLE feeds ADD COLUMN failures INTEGER NOT NULL DEFAULT 0 CHECK(failures BETWEEN 0 AND 20);
CREATE TABLE article_copies (
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 entry_id INTEGER NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
 body TEXT NOT NULL, body_html TEXT NOT NULL, fetched INTEGER NOT NULL,
 PRIMARY KEY(user_id,entry_id)
);
-- Older entries have no markup, so conditional 304 responses cannot enrich them.
-- Fetch complete bodies once after this migration while retaining reading state.
UPDATE feeds SET etag='',modified='',checked=0,next_check=0;
