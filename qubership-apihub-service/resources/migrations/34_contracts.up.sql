-- DDL contract tables (tables AND views — kind discriminates)
CREATE TABLE IF NOT EXISTS ddl_tables
(
    package_id   varchar NOT NULL,
    version      varchar NOT NULL,
    revision     integer NOT NULL,
    ddl_table_id varchar NOT NULL,
    kind         varchar NOT NULL CHECK (kind IN ('table', 'view')),
    title        varchar,
    schema_name  varchar,
    name         varchar,
    deprecated   boolean NOT NULL DEFAULT false,
    metadata     jsonb,
    data_hash    varchar,
    document_id  varchar,
    CONSTRAINT pk_ddl_tables PRIMARY KEY (package_id, version, revision, ddl_table_id)
);

CREATE INDEX IF NOT EXISTS ddl_tables_kind_idx ON ddl_tables (package_id, version, revision, kind);
CREATE INDEX IF NOT EXISTS ddl_tables_document_id_idx ON ddl_tables (document_id);

-- Deduplicated full DDL entity payload
CREATE TABLE IF NOT EXISTS ddl_table_data
(
    data_hash varchar PRIMARY KEY,
    data      bytea
);

-- Per-entity DDL diff details
CREATE TABLE IF NOT EXISTS ddl_comparison
(
    package_id             varchar NOT NULL,
    version                varchar NOT NULL,
    revision               integer NOT NULL,
    previous_package_id    varchar NOT NULL,
    previous_version       varchar NOT NULL,
    previous_revision      integer NOT NULL,
    ddl_table_id           varchar NOT NULL,
    previous_ddl_table_id  varchar NOT NULL,
    comparison_id          varchar,
    data_hash              varchar,
    previous_data_hash     varchar,
    changes_summary        jsonb,
    changes                jsonb,
    CONSTRAINT pk_ddl_comparison PRIMARY KEY (package_id, version, revision,
                                              previous_package_id, previous_version, previous_revision,
                                              ddl_table_id, previous_ddl_table_id)
);

CREATE INDEX IF NOT EXISTS ddl_comparison_comparison_id_idx ON ddl_comparison (comparison_id);

-- FTS source for DDL
CREATE TABLE IF NOT EXISTS fts_ddl_search_text
(
    package_id       varchar  NOT NULL,
    version          varchar  NOT NULL,
    revision         integer  NOT NULL,
    ddl_table_id     varchar  NOT NULL,
    status           varchar  NOT NULL,
    kind             varchar  NOT NULL,
    search_data_hash varchar,
    data_vector      tsvector,
    CONSTRAINT pk_fts_ddl_search_text PRIMARY KEY (package_id, version, revision, ddl_table_id)
);

CREATE INDEX IF NOT EXISTS fts_ddl_search_text_data_vector_idx ON fts_ddl_search_text USING gin (data_vector);

-- MCP contract entities
CREATE TABLE IF NOT EXISTS mcp_entities
(
    package_id    varchar NOT NULL,
    version       varchar NOT NULL,
    revision      integer NOT NULL,
    mcp_entity_id varchar NOT NULL,
    kind          varchar NOT NULL CHECK (kind IN ('init', 'tool', 'prompt', 'resource')),
    title         varchar,
    mcp_endpoint  varchar NOT NULL,
    server_name   varchar NOT NULL,
    deprecated    boolean NOT NULL DEFAULT false,
    metadata      jsonb,
    data_hash     varchar,
    document_id   varchar,
    CONSTRAINT pk_mcp_entities PRIMARY KEY (package_id, version, revision, mcp_entity_id)
);

CREATE INDEX IF NOT EXISTS mcp_entities_server_name_idx ON mcp_entities (package_id, version, revision, server_name);
CREATE INDEX IF NOT EXISTS mcp_entities_kind_idx ON mcp_entities (package_id, version, revision, kind);
CREATE INDEX IF NOT EXISTS mcp_entities_document_id_idx ON mcp_entities (document_id);

-- Deduplicated full MCP entity payload
CREATE TABLE IF NOT EXISTS mcp_entity_data
(
    data_hash varchar PRIMARY KEY,
    data      bytea
);

-- FTS source for MCP
CREATE TABLE IF NOT EXISTS fts_mcp_search_text
(
    package_id       varchar  NOT NULL,
    version          varchar  NOT NULL,
    revision         integer  NOT NULL,
    mcp_entity_id    varchar  NOT NULL,
    status           varchar  NOT NULL,
    kind             varchar  NOT NULL,
    search_data_hash varchar,
    data_vector      tsvector,
    CONSTRAINT pk_fts_mcp_search_text PRIMARY KEY (package_id, version, revision, mcp_entity_id)
);

CREATE INDEX IF NOT EXISTS fts_mcp_search_text_data_vector_idx ON fts_mcp_search_text USING gin (data_vector);
