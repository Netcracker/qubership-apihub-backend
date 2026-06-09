package entity

import "github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"

type DDLContractEntity struct {
	tableName struct{} `pg:"ddl_tables"`

	PackageId  string   `pg:"package_id, pk, type:varchar"`
	Version    string   `pg:"version, pk, type:varchar"`
	Revision   int      `pg:"revision, pk, type:integer"`
	DdlTableId string   `pg:"ddl_table_id, pk, type:varchar"`
	Kind       string   `pg:"kind, type:varchar, use_zero"`
	SchemaName string   `pg:"schema_name, type:varchar, use_zero"`
	Name       string   `pg:"name, type:varchar, use_zero"`
	Metadata   Metadata `pg:"metadata, type:jsonb"`
	DataHash   *string  `pg:"data_hash, type:varchar"`
	DocumentId string   `pg:"document_id, type:varchar, use_zero"`
}

type DDLContractDataEntity struct {
	tableName struct{} `pg:"ddl_table_data, alias:ddl_table_data"`

	DataHash string `pg:"data_hash, pk, type:varchar"`
	Data     []byte `pg:"data, type:bytea"`
}

type DDLContractComparisonEntity struct {
	tableName struct{} `pg:"ddl_comparison"`

	PackageId          string             `pg:"package_id, type:varchar, use_zero"`
	Version            string             `pg:"version, type:varchar, use_zero"`
	Revision           int                `pg:"revision, type:integer, use_zero"`
	PreviousPackageId  string             `pg:"previous_package_id, type:varchar, use_zero"`
	PreviousVersion    string             `pg:"previous_version, type:varchar, use_zero"`
	PreviousRevision   int                `pg:"previous_revision, type:integer, use_zero"`
	DdlTableId         string             `pg:"ddl_table_id, type:varchar"`
	PreviousDdlTableId string             `pg:"previous_ddl_table_id, type:varchar"`
	ComparisonId       string             `pg:"comparison_id, type:varchar"`
	DataHash           *string            `pg:"data_hash, type:varchar"`
	PreviousDataHash   *string            `pg:"previous_data_hash, type:varchar"`
	ChangesSummary     view.ChangeSummary `pg:"changes_summary, type:jsonb"`
	Changes            interface{}        `pg:"changes, type:jsonb"`
}

type DDLContractSearchTextEntity struct {
	// no go-pg mapping due to different insert/lookup process

	PackageId      string
	Version        string
	Revision       int
	DdlTableId     string
	Status         string
	Kind           string
	SearchDataHash string
	SearchTextData []byte
}

type DDLContractKindCountEntity struct {
	Kind  string `pg:"kind, type:varchar"`
	Count int    `pg:"count, type:integer"`
}

func MakeDdlTableView(ent *DDLContractEntity, data []byte) *view.DdlTableView {
	result := &view.DdlTableView{
		TableId:    ent.DdlTableId,
		Kind:       ent.Kind,
		SchemaName: ent.SchemaName,
		TableName:  ent.Name,
		DocumentId: ent.DocumentId,
		PackageRef: view.MakePackageRefKey(ent.PackageId, ent.Version, ent.Revision),
		Metadata:   ent.Metadata,
	}
	if len(data) > 0 {
		result.Data = string(data)
	}
	return result
}
