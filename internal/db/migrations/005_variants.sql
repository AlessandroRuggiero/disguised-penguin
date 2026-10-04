CREATE TABLE IF NOT EXISTS variants (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cli_name TEXT NOT NULL,
    project_dir TEXT NOT NULL,
    image TEXT NOT NULL,
    built_at INTEGER NOT NULL,
    last_used_at INTEGER,
    UNIQUE(cli_name, project_dir)
);
