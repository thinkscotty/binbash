CREATE VIRTUAL TABLE items_fts USING fts5(
    name, description, keywords,
    content='items', content_rowid='id',
    tokenize = 'porter unicode61 remove_diacritics 2'
);

CREATE TRIGGER items_ai AFTER INSERT ON items BEGIN
    INSERT INTO items_fts(rowid, name, description, keywords)
    VALUES (new.id, new.name, new.description, new.keywords);
END;

CREATE TRIGGER items_ad AFTER DELETE ON items BEGIN
    INSERT INTO items_fts(items_fts, rowid, name, description, keywords)
    VALUES ('delete', old.id, old.name, old.description, old.keywords);
END;

CREATE TRIGGER items_au AFTER UPDATE ON items BEGIN
    INSERT INTO items_fts(items_fts, rowid, name, description, keywords)
    VALUES ('delete', old.id, old.name, old.description, old.keywords);
    INSERT INTO items_fts(rowid, name, description, keywords)
    VALUES (new.id, new.name, new.description, new.keywords);
END;

INSERT INTO items_fts(rowid, name, description, keywords)
SELECT id, name, description, keywords FROM items;
