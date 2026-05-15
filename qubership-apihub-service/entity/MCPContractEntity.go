package entity

type MCPContractEntity struct {
	tableName struct{} `pg:"mcp_entities"`

	PackageId    string   `pg:"package_id, pk, type:varchar"`
	Version      string   `pg:"version, pk, type:varchar"`
	Revision     int      `pg:"revision, pk, type:integer"`
	McpEntityId  string   `pg:"mcp_entity_id, pk, type:varchar"`
	Kind         string   `pg:"kind, type:varchar, use_zero"`
	Title        string   `pg:"title, type:varchar, use_zero"`
	McpEndpoint  string   `pg:"mcp_endpoint, type:varchar, use_zero"`
	ServerName   string   `pg:"server_name, type:varchar, use_zero"`
	Deprecated   bool     `pg:"deprecated, type:boolean, use_zero"`
	Metadata     Metadata `pg:"metadata, type:jsonb"`
	DataHash     *string  `pg:"data_hash, type:varchar"`
	DocumentId   string   `pg:"document_id, type:varchar, use_zero"`
}

type MCPContractDataEntity struct {
	tableName struct{} `pg:"mcp_entity_data, alias:mcp_entity_data"`

	DataHash string `pg:"data_hash, pk, type:varchar"`
	Data     []byte `pg:"data, type:bytea"`
}

type MCPContractSearchTextEntity struct {
	tableName struct{} `pg:"fts_mcp_search_text"`

	PackageId      string `pg:"package_id, type:varchar"`
	Version        string `pg:"version, type:varchar"`
	Revision       int    `pg:"revision, type:integer"`
	McpEntityId    string `pg:"mcp_entity_id, type:varchar"`
	Status         string `pg:"status, type:varchar"`
	Kind           string `pg:"kind, type:varchar"`
	SearchDataHash string `pg:"search_data_hash, type:varchar"`
	DataVector     string `pg:"data_vector, type:tsvector"`
}

type MCPContractKindCountEntity struct {
	Kind  string `pg:"kind, type:varchar"`
	Count int    `pg:"count, type:integer"`
}
