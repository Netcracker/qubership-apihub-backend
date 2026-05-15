package view

type DdlTableListView struct {
	Tables []interface{} `json:"tables"`
}

type DdlTableView struct {
	TableId    string      `json:"tableId"`
	Title      string      `json:"title,omitempty"`
	Kind       string      `json:"kind"`
	SchemaName string      `json:"schemaName,omitempty"`
	TableName  string      `json:"tableName,omitempty"`
	Deprecated bool        `json:"deprecated"`
	DocumentId string      `json:"documentId,omitempty"`
	PackageRef string      `json:"packageRef,omitempty"`
	Metadata   interface{} `json:"metadata,omitempty"`
}

type DdlTableDetailView struct {
	DdlTableView
	Data interface{} `json:"data,omitempty"`
}

type DdlTableChangesView struct {
	Changes        []interface{} `json:"changes"`
	ChangesSummary ChangeSummary `json:"changesSummary"`
}

const DdlKindTable = "table"
const DdlKindView = "view"
