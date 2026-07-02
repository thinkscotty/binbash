CREATE TABLE backup_state (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    last_backup_at DATETIME,
    last_backup_max_item_id INTEGER NOT NULL DEFAULT 0
);

INSERT INTO backup_state (id, last_backup_at, last_backup_max_item_id) VALUES (1, NULL, 0);
