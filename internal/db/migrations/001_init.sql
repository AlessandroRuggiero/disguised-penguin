CREATE TABLE IF NOT EXISTS clis (
    id INTEGER PRIMARY KEY AUTOINCREMENT, 
    name TEXT UNIQUE, 
    container_name TEXT,
    config_mounts TEXT,
    port_mappings TEXT
);

CREATE TABLE IF NOT EXISTS registries (
    id INTEGER PRIMARY KEY AUTOINCREMENT, 
    uri TEXT UNIQUE,
    registry_type TEXT,
    priority INTEGER DEFAULT 0,
    name TEXT UNIQUE
);

CREATE TABLE IF NOT EXISTS workspaces (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT UNIQUE
);

INSERT INTO registries (uri, registry_type, priority, name)
SELECT 'https://raw.githubusercontent.com/AlessandroRuggiero/disguised-penguin-repo/main', 'github', 0, 'default'
WHERE NOT EXISTS (SELECT 1 FROM registries);

INSERT INTO workspaces (name)
SELECT 'default'
WHERE NOT EXISTS (SELECT 1 FROM workspaces);