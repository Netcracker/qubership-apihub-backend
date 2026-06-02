package repository

import (
	"fmt"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/db"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/entity"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
	"github.com/go-pg/pg/v10"
)

type DDLContractRepository interface {
	ListDdlTables(packageId, version string, revision int, kind, textFilter string, limit, offset int) ([]*entity.DDLContractEntity, error)
	GetDdlTable(packageId, version string, revision int, ddlTableId string, includeData bool) (*entity.DDLContractEntity, []byte, error)
	GetDdlTableChanges(packageId, version string, revision int, ddlTableId string) (*entity.DDLContractComparisonEntity, error)
	GetEntitiesCount(packageId, version string, revision int) ([]entity.DDLContractKindCountEntity, error)
	GetComparisonSummary(comparisonId string) (*view.ChangeSummary, error)
	GlobalSearchForDDL(searchQuery *entity.GlobalContractSearchQuery) ([]entity.DDLContractSearchResult, error)

	CreateDdlContracts(contracts []*entity.DDLContractEntity) error
	CreateDdlContractData(data []*entity.DDLContractDataEntity) error
	CreateDdlContractComparisons(comparisons []*entity.DDLContractComparisonEntity) error
	CreateDdlContractSearchText(texts []*entity.DDLContractSearchTextEntity) error
	DeleteDdlContractsByRevision(packageId, version string, revision int) error
}

type ddlContractRepositoryImpl struct {
	cp db.ConnectionProvider
}

func NewDDLContractRepository(cp db.ConnectionProvider) DDLContractRepository {
	return &ddlContractRepositoryImpl{cp: cp}
}

func (r *ddlContractRepositoryImpl) ListDdlTables(packageId, version string, revision int, kind, textFilter string, limit, offset int) ([]*entity.DDLContractEntity, error) {
	var result []*entity.DDLContractEntity
	query := r.cp.GetConnection().Model(&result).
		Where("package_id = ?", packageId).
		Where("version = ?", version).
		Where("revision = ?", revision)
	if kind != "" {
		query = query.Where("kind = ?", kind)
	}
	if textFilter != "" {
		query = query.Where("name ILIKE ?", fmt.Sprintf("%%%s%%", textFilter))
	}
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}
	err := query.Select()
	if err != nil {
		if err == pg.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return result, nil
}

func (r *ddlContractRepositoryImpl) GetDdlTable(packageId, version string, revision int, ddlTableId string, includeData bool) (*entity.DDLContractEntity, []byte, error) {
	conn := r.cp.GetConnection()
	ent := new(entity.DDLContractEntity)
	err := conn.Model(ent).
		Where("package_id = ?", packageId).
		Where("version = ?", version).
		Where("revision = ?", revision).
		Where("ddl_table_id = ?", ddlTableId).
		First()
	if err != nil {
		if err == pg.ErrNoRows {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	var data []byte
	if includeData && ent.DataHash != nil {
		dataEnt := new(entity.DDLContractDataEntity)
		err = conn.Model(dataEnt).Where("data_hash = ?", *ent.DataHash).First()
		if err != nil {
			if err == pg.ErrNoRows {
				return nil, nil, fmt.Errorf("no data found for ddl table %s data hash = %s", ddlTableId, ent.DataHash)
			}
			return nil, nil, err
		}
		data = dataEnt.Data
	}
	return ent, data, nil
}

func (r *ddlContractRepositoryImpl) GetDdlTableChanges(packageId, version string, revision int, ddlTableId string) (*entity.DDLContractComparisonEntity, error) {
	ent := new(entity.DDLContractComparisonEntity)
	err := r.cp.GetConnection().Model(ent).
		Where("package_id = ?", packageId).
		Where("version = ?", version).
		Where("revision = ?", revision).
		Where("ddl_table_id = ?", ddlTableId).
		First()
	if err != nil {
		if err == pg.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return ent, nil
}

func (r *ddlContractRepositoryImpl) GetEntitiesCount(packageId, version string, revision int) ([]entity.DDLContractKindCountEntity, error) {
	var result []entity.DDLContractKindCountEntity
	_, err := r.cp.GetConnection().Query(&result,
		`SELECT kind, count(*) as count FROM ddl_tables WHERE package_id=? AND version=? AND revision=? GROUP BY kind`,
		packageId, version, revision)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (r *ddlContractRepositoryImpl) GetComparisonSummary(comparisonId string) (*view.ChangeSummary, error) {
	type row struct {
		ChangesSummary view.ChangeSummary `pg:"changes_summary"`
	}
	var rows []row
	_, err := r.cp.GetConnection().Query(&rows,
		`SELECT changes_summary FROM ddl_comparison WHERE comparison_id=?`, comparisonId)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	result := &view.ChangeSummary{}
	for _, row := range rows {
		result.Breaking += row.ChangesSummary.Breaking
		result.SemiBreaking += row.ChangesSummary.SemiBreaking
		result.Deprecated += row.ChangesSummary.Deprecated
		result.NonBreaking += row.ChangesSummary.NonBreaking
		result.Annotation += row.ChangesSummary.Annotation
		result.Unclassified += row.ChangesSummary.Unclassified
	}
	return result, nil
}

func (r *ddlContractRepositoryImpl) GlobalSearchForDDL(searchQuery *entity.GlobalContractSearchQuery) ([]entity.DDLContractSearchResult, error) {
	_, err := r.cp.GetConnection().Exec("select websearch_to_tsquery(?)", searchQuery.OriginalTextInput)
	if err != nil {
		return nil, fmt.Errorf("invalid search string: %v", err.Error())
	}
	var result []entity.DDLContractSearchResult
	ddlSearchQuery := `
select
    dt.package_id,
    pg.name,
    dt.version,
    dt.revision,
    pv.status,
    dt.ddl_table_id,
    dt.kind,
    dt.schema_name,
    dt.name,
    parent_package_names(dt.package_id) parent_names
from ddl_tables dt
         inner join (
    SELECT DISTINCT ON (rank, package_id, ddl_table_id)
        ts_rank(data_vector, search_query) as rank,
        ts.package_id   as package_id,
        ts.ddl_table_id as ddl_table_id,
        ts.version      as version,
        ts.revision     as revision

    FROM fts_ddl_search_text ts,
         websearch_to_tsquery(?original_text_input) search_query
    WHERE ts.status = ?status
        and (?kinds = '{}' or ts.kind = ANY(?kinds::text[]))
        and (?versions = '{}' or version like ANY(
						select id from unnest(?versions::text[]) id))
        and (package_id like ANY(
						select id from unnest(?packages::text[]) id
						union
						select id||'.%' from unnest(?packages::text[]) id))
        and search_query @@ data_vector
    ORDER BY ts_rank(data_vector, search_query) DESC,
             package_id,
             ddl_table_id desc,
             version DESC,
             revision DESC
    LIMIT ?limit OFFSET ?offset
) all_ts
                   on all_ts.package_id = dt.package_id and
                      all_ts.version = dt.version and
                      all_ts.revision = dt.revision and
                      all_ts.ddl_table_id = dt.ddl_table_id

inner join published_version pv on dt.package_id=pv.package_id and dt.version=pv.version and dt.revision=pv.revision
inner join package_group pg on dt.package_id=pg.id

where all_ts.rank > 0
and pv.deleted_at is null
and pv.published_at >= ?start_date
and pv.published_at <= ?end_date
order by all_ts.rank desc, dt.ddl_table_id
limit ?limit;
`
	_, err = r.cp.GetConnection().Model(searchQuery).Query(&result, ddlSearchQuery)
	if err != nil {
		if err == pg.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return result, nil
}

func (r *ddlContractRepositoryImpl) CreateDdlContracts(contracts []*entity.DDLContractEntity) error {
	if len(contracts) == 0 {
		return nil
	}
	_, err := r.cp.GetConnection().Model(&contracts).OnConflict("(package_id, version, revision, ddl_table_id) DO UPDATE").Insert()
	return err
}

func (r *ddlContractRepositoryImpl) CreateDdlContractData(data []*entity.DDLContractDataEntity) error {
	if len(data) == 0 {
		return nil
	}
	_, err := r.cp.GetConnection().Model(&data).OnConflict("(data_hash) DO NOTHING").Insert()
	return err
}

func (r *ddlContractRepositoryImpl) CreateDdlContractComparisons(comparisons []*entity.DDLContractComparisonEntity) error {
	if len(comparisons) == 0 {
		return nil
	}
	_, err := r.cp.GetConnection().Model(&comparisons).
		OnConflict("(package_id, version, revision, previous_package_id, previous_version, previous_revision, ddl_table_id, previous_ddl_table_id) DO UPDATE").
		Insert()
	return err
}

func (r *ddlContractRepositoryImpl) CreateDdlContractSearchText(texts []*entity.DDLContractSearchTextEntity) error {
	if len(texts) == 0 {
		return nil
	}
	_, err := r.cp.GetConnection().Model(&texts).OnConflict("(package_id, version, revision, ddl_table_id) DO UPDATE").Insert()
	return err
}

func (r *ddlContractRepositoryImpl) DeleteDdlContractsByRevision(packageId, version string, revision int) error {
	_, err := r.cp.GetConnection().Model(&entity.DDLContractEntity{}).
		Where("package_id = ?", packageId).
		Where("version = ?", version).
		Where("revision = ?", revision).
		Delete()
	return err
}
