CREATE TABLE IF NOT EXISTS mount_protection (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id INTEGER NOT NULL,
    mount_path TEXT NOT NULL,
    permission TEXT NOT NULL,
    UNIQUE(workspace_id, mount_path),
    FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
);