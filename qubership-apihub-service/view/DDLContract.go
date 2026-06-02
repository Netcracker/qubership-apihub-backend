package view

type DdlTableListView struct {
	Tables []interface{} `json:"tables"`
}

type DdlTableView struct {
	TableId    string      `json:"tableId"`
	Kind       string      `json:"kind"`
	SchemaName string      `json:"schemaName,omitempty"`
	TableName  string      `json:"tableName,omitempty"`
	DocumentId string      `json:"documentId,omitempty"`
	PackageRef string      `json:"packageRef,omitempty"`
	Metadata   interface{} `json:"metadata,omitempty"`
	Data       string      `json:"data,omitempty"`
}

type DdlTableChangesView struct {
	Changes        []interface{} `json:"changes"`
	ChangesSummary ChangeSummary `json:"changesSummary"`
}

const DdlKindTable = "table"
const DdlKindView = "view"

type DdlContractSearchResult struct {
	PackageId      string   `json:"packageId"`
	PackageName    string   `json:"name"`
	ParentPackages []string `json:"parentPackages"`
	VersionStatus  string   `json:"status"`
	Version        string   `json:"version"`
	TableId        string   `json:"tableId"`
	Kind           string   `json:"kind"`
	SchemaName     string   `json:"schemaName,omitempty"`
	TableName      string   `json:"tableName,omitempty"`
}
