-- Skip: a per-member "not for me" marker. A skipped story leaves the reading
-- table without being marked read or saved.
ALTER TABLE states ADD COLUMN skipped INTEGER NOT NULL DEFAULT 0 CHECK(skipped IN(0,1));
