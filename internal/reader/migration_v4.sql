ALTER TABLE entries ADD COLUMN image_url TEXT NOT NULL DEFAULT '';
CREATE TABLE instance_options (
 id INTEGER PRIMARY KEY CHECK(id=1),
 images_enabled INTEGER NOT NULL DEFAULT 0 CHECK(images_enabled IN(0,1))
);
INSERT INTO instance_options(id) VALUES(1);
