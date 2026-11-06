CREATE TABLE IF NOT EXISTS catalog_categories (
 name TEXT PRIMARY KEY CHECK(length(name) BETWEEN 1 AND 40)
);
