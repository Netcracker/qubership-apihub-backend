package repository

import (
	"fmt"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/db"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/entity"
	"github.com/go-pg/pg/v10"
)

type MCPContractRepository interface {
	ListMcpEntities(packageId, version string, revision int, kind, textFilter string, limit, offset int) ([]*entity.MCPContractEntity, error)
	GetMcpEntity(packageId, version string, revision int, mcpEntityId string) (*entity.MCPContractEntity, []byte, error)
	GetEntitiesCount(packageId, version string, revision int) ([]entity.MCPContractKindCountEntity, error)

	CreateMcpContracts(contracts []*entity.MCPContractEntity) error
	CreateMcpContractData(data []*entity.MCPContractDataEntity) error
	CreateMcpContractSearchText(texts []*entity.MCPContractSearchTextEntity) error
	DeleteMcpContractsByRevision(packageId, version string, revision int) error
}

type mcpContractRepositoryImpl struct {
	cp db.ConnectionProvider
}

func NewMCPContractRepository(cp db.ConnectionProvider) MCPContractRepository {
	return &mcpContractRepositoryImpl{cp: cp}
}

func (r *mcpContractRepositoryImpl) ListMcpEntities(packageId, version string, revision int, kind, textFilter string, limit, offset int) ([]*entity.MCPContractEntity, error) {
	var result []*entity.MCPContractEntity
	query := r.cp.GetConnection().Model(&result).
		Where("package_id = ?", packageId).
		Where("version = ?", version).
		Where("revision = ?", revision)
	if kind != "" {
		query = query.Where("kind = ?", kind)
	}
	if textFilter != "" {
		query = query.Where("mcp_entity_id ILIKE ?", fmt.Sprintf("%%%s%%", textFilter))
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

func (r *mcpContractRepositoryImpl) GetMcpEntity(packageId, version string, revision int, mcpEntityId string) (*entity.MCPContractEntity, []byte, error) {
	conn := r.cp.GetConnection()
	ent := new(entity.MCPContractEntity)
	err := conn.Model(ent).
		Where("package_id = ?", packageId).
		Where("version = ?", version).
		Where("revision = ?", revision).
		Where("mcp_entity_id = ?", mcpEntityId).
		First()
	if err != nil {
		if err == pg.ErrNoRows {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	var data []byte
	if ent.DataHash != nil {
		dataEnt := new(entity.MCPContractDataEntity)
		err = conn.Model(dataEnt).Where("data_hash = ?", *ent.DataHash).First()
		if err == nil {
			data = dataEnt.Data
		}
	}
	return ent, data, nil
}

func (r *mcpContractRepositoryImpl) GetEntitiesCount(packageId, version string, revision int) ([]entity.MCPContractKindCountEntity, error) {
	var result []entity.MCPContractKindCountEntity
	_, err := r.cp.GetConnection().Query(&result,
		`SELECT kind, count(*) as count FROM mcp_entities WHERE package_id=? AND version=? AND revision=? GROUP BY kind`,
		packageId, version, revision)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (r *mcpContractRepositoryImpl) CreateMcpContracts(contracts []*entity.MCPContractEntity) error {
	if len(contracts) == 0 {
		return nil
	}
	_, err := r.cp.GetConnection().Model(&contracts).OnConflict("(package_id, version, revision, mcp_entity_id) DO UPDATE").Insert()
	return err
}

func (r *mcpContractRepositoryImpl) CreateMcpContractData(data []*entity.MCPContractDataEntity) error {
	if len(data) == 0 {
		return nil
	}
	_, err := r.cp.GetConnection().Model(&data).OnConflict("(data_hash) DO NOTHING").Insert()
	return err
}

func (r *mcpContractRepositoryImpl) CreateMcpContractSearchText(texts []*entity.MCPContractSearchTextEntity) error {
	if len(texts) == 0 {
		return nil
	}
	_, err := r.cp.GetConnection().Model(&texts).OnConflict("(package_id, version, revision, mcp_entity_id) DO UPDATE").Insert()
	return err
}

func (r *mcpContractRepositoryImpl) DeleteMcpContractsByRevision(packageId, version string, revision int) error {
	_, err := r.cp.GetConnection().Model(&entity.MCPContractEntity{}).
		Where("package_id = ?", packageId).
		Where("version = ?", version).
		Where("revision = ?", revision).
		Delete()
	return err
}
