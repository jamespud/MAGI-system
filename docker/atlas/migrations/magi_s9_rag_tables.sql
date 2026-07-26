-- MAGI S9: RAG hierarchy tables (1800/900/300 parent-child for hybrid retrieval).
-- Mirrors the GORM models in adapter/rag/repository.go for the production atlas path.

CREATE TABLE IF NOT EXISTS rag_chunk_1800 (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    source VARCHAR(32) NOT NULL DEFAULT '',
    source_ref VARCHAR(128) NOT NULL DEFAULT '',
    content MEDIUMTEXT NOT NULL,
    token_count INT NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_source (source, source_ref)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS rag_chunk_900 (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    parent_1800_id VARCHAR(64) NOT NULL DEFAULT '',
    source VARCHAR(32) NOT NULL DEFAULT '',
    source_ref VARCHAR(128) NOT NULL DEFAULT '',
    content TEXT NOT NULL,
    token_count INT NOT NULL DEFAULT 0,
    seq INT NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_parent_1800 (parent_1800_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS rag_chunk_300 (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    parent_900_id VARCHAR(64) NOT NULL DEFAULT '',
    source VARCHAR(32) NOT NULL DEFAULT '',
    source_ref VARCHAR(128) NOT NULL DEFAULT '',
    content TEXT NOT NULL,
    token_count INT NOT NULL DEFAULT 0,
    seq INT NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_parent_900 (parent_900_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
