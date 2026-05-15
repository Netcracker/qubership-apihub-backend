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
	GetDdlTable(packageId, version string, revision int, ddlTableId string) (*entity.DDLContractEntity, []byte, error)
	GetDdlTableChanges(packageId, version string, revision int, ddlTableId string) (*entity.DDLContractComparisonEntity, error)
	GetEntitiesCount(packageId, version string, revision int) ([]entity.DDLContractKindCountEntity, error)
	GetDeprecatedCount(packageId, version string, revision int) (int, error)
	GetComparisonSummary(comparisonId string) ([]view.DDLEntityKindSummary, error)

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
		query = query.Where("title ILIKE ?", fmt.Sprintf("%%%s%%", textFilter))
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

func (r *ddlContractRepositoryImpl) GetDdlTable(packageId, version string, revision int, ddlTableId string) (*entity.DDLContractEntity, []byte, error) {
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
	if ent.DataHash != nil {
		dataEnt := new(entity.DDLContractDataEntity)
		err = conn.Model(dataEnt).Where("data_hash = ?", *ent.DataHash).First()
		if err == nil {
			data = dataEnt.Data
		}
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

func (r *ddlContractRepositoryImpl) GetDeprecatedCount(packageId, version string, revision int) (int, error) {
	var result []*entity.DDLContractEntity
	count, err := r.cp.GetConnection().Model(&result).
		Where("package_id = ?", packageId).
		Where("version = ?", version).
		Where("revision = ?", revision).
		Where("deprecated = true").
		Count()
	return count, err
}

func (r *ddlContractRepositoryImpl) GetComparisonSummary(comparisonId string) ([]view.DDLEntityKindSummary, error) {
	type row struct {
		Kind           string             `pg:"kind"`
		ChangesSummary view.ChangeSummary `pg:"changes_summary"`
	}
	var rows []row
	_, err := r.cp.GetConnection().Query(&rows,
		`SELECT dt.kind, dc.changes_summary FROM ddl_comparison dc
         JOIN ddl_tables dt ON dc.package_id=dt.package_id AND dc.version=dt.version AND dc.revision=dt.revision AND dc.ddl_table_id=dt.ddl_table_id
         WHERE dc.comparison_id=?`, comparisonId)
	if err != nil {
		return nil, err
	}
	kindMap := map[string]*view.DDLEntityKindSummary{}
	for _, row := range rows {
		s, ok := kindMap[row.Kind]
		if !ok {
			s = &view.DDLEntityKindSummary{EntityKind: row.Kind}
			kindMap[row.Kind] = s
		}
		s.ChangesSummary.Breaking += row.ChangesSummary.Breaking
		s.ChangesSummary.SemiBreaking += row.ChangesSummary.SemiBreaking
		s.ChangesSummary.Deprecated += row.ChangesSummary.Deprecated
		s.ChangesSummary.NonBreaking += row.ChangesSummary.NonBreaking
		s.ChangesSummary.Annotation += row.ChangesSummary.Annotation
		s.ChangesSummary.Unclassified += row.ChangesSummary.Unclassified
	}
	result := make([]view.DDLEntityKindSummary, 0, len(kindMap))
	for _, v := range kindMap {
		result = append(result, *v)
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
