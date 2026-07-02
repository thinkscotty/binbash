CREATE TABLE auth_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    password_hash TEXT NOT NULL,
    session_key TEXT NOT NULL
);
