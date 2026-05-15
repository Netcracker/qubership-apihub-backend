package service

import (
	"encoding/json"
	"net/http"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/entity"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/exception"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/repository"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
)

type DDLContractService interface {
	ListDdlTables(packageId, versionName, kind, textFilter string, limit, offset int) (*view.DdlTableListView, error)
	GetDdlTable(packageId, versionName, ddlTableId string) (interface{}, error)
	GetDdlTableChanges(packageId, versionName, ddlTableId string) (*view.DdlTableChangesView, error)
	GetVersionSummary(packageId, versionName string) (*view.VersionDDLContractsSummary, error)
	GetChangesSummary(comparisonId string) (*view.DDLContractsSummary, error)
}

func NewDDLContractService(ddlRepo repository.DDLContractRepository, publishedRepo repository.PublishedRepository) DDLContractService {
	return &ddlContractServiceImpl{ddlRepo: ddlRepo, publishedRepo: publishedRepo}
}

type ddlContractServiceImpl struct {
	ddlRepo       repository.DDLContractRepository
	publishedRepo repository.PublishedRepository
}

func (s *ddlContractServiceImpl) resolveRevision(packageId, versionName string) (string, int, error) {
	version, err := s.publishedRepo.GetVersion(packageId, versionName)
	if err != nil {
		return "", 0, err
	}
	if version == nil {
		return "", 0, &exception.CustomError{
			Status:  http.StatusNotFound,
			Code:    exception.PublishedVersionNotFound,
			Message: exception.PublishedVersionNotFoundMsg,
			Params:  map[string]interface{}{"version": versionName},
		}
	}
	return version.Version, version.Revision, nil
}

func (s *ddlContractServiceImpl) ListDdlTables(packageId, versionName, kind, textFilter string, limit, offset int) (*view.DdlTableListView, error) {
	version, revision, err := s.resolveRevision(packageId, versionName)
	if err != nil {
		return nil, err
	}
	entities, err := s.ddlRepo.ListDdlTables(packageId, version, revision, kind, textFilter, limit, offset)
	if err != nil {
		return nil, err
	}
	result := &view.DdlTableListView{Tables: make([]interface{}, 0, len(entities))}
	for _, ent := range entities {
		result.Tables = append(result.Tables, makeDdlTableView(ent, packageId, version, revision))
	}
	return result, nil
}

func (s *ddlContractServiceImpl) GetDdlTable(packageId, versionName, ddlTableId string) (interface{}, error) {
	version, revision, err := s.resolveRevision(packageId, versionName)
	if err != nil {
		return nil, err
	}
	ent, data, err := s.ddlRepo.GetDdlTable(packageId, version, revision, ddlTableId)
	if err != nil {
		return nil, err
	}
	if ent == nil {
		return nil, &exception.CustomError{
			Status:  http.StatusNotFound,
			Code:    exception.PublishedVersionNotFound,
			Message: "DDL table not found",
			Params:  map[string]interface{}{"ddlTableId": ddlTableId},
		}
	}
	detail := view.DdlTableDetailView{DdlTableView: *makeDdlTableView(ent, packageId, version, revision)}
	if len(data) > 0 {
		var parsed interface{}
		if err := json.Unmarshal(data, &parsed); err == nil {
			detail.Data = parsed
		}
	}
	return detail, nil
}

func (s *ddlContractServiceImpl) GetDdlTableChanges(packageId, versionName, ddlTableId string) (*view.DdlTableChangesView, error) {
	version, revision, err := s.resolveRevision(packageId, versionName)
	if err != nil {
		return nil, err
	}
	ent, err := s.ddlRepo.GetDdlTableChanges(packageId, version, revision, ddlTableId)
	if err != nil {
		return nil, err
	}
	if ent == nil {
		return &view.DdlTableChangesView{Changes: []interface{}{}}, nil
	}
	var changes []interface{}
	if ent.Changes != nil {
		if list, ok := ent.Changes.([]interface{}); ok {
			changes = list
		}
	}
	return &view.DdlTableChangesView{
		Changes:        changes,
		ChangesSummary: ent.ChangesSummary,
	}, nil
}

func (s *ddlContractServiceImpl) GetVersionSummary(packageId, versionName string) (*view.VersionDDLContractsSummary, error) {
	version, revision, err := s.resolveRevision(packageId, versionName)
	if err != nil {
		return nil, err
	}
	counts, err := s.ddlRepo.GetEntitiesCount(packageId, version, revision)
	if err != nil {
		return nil, err
	}
	if len(counts) == 0 {
		return nil, nil
	}
	deprecated, err := s.ddlRepo.GetDeprecatedCount(packageId, version, revision)
	if err != nil {
		return nil, err
	}
	summary := &view.VersionDDLContractsSummary{Deprecated: deprecated}
	for _, c := range counts {
		switch c.Kind {
		case view.DdlKindTable:
			summary.Tables = c.Count
		case view.DdlKindView:
			summary.Views = c.Count
		}
	}
	return summary, nil
}

func (s *ddlContractServiceImpl) GetChangesSummary(comparisonId string) (*view.DDLContractsSummary, error) {
	kinds, err := s.ddlRepo.GetComparisonSummary(comparisonId)
	if err != nil {
		return nil, err
	}
	if len(kinds) == 0 {
		return nil, nil
	}
	return &view.DDLContractsSummary{EntityKinds: kinds}, nil
}

func makeDdlTableView(ent *entity.DDLContractEntity, packageId, version string, revision int) *view.DdlTableView {
	return &view.DdlTableView{
		TableId:    ent.DdlTableId,
		Title:      ent.Title,
		Kind:       ent.Kind,
		SchemaName: ent.SchemaName,
		TableName:  ent.Name,
		Deprecated: ent.Deprecated,
		DocumentId: ent.DocumentId,
		PackageRef: view.MakePackageRefKey(packageId, version, revision),
		Metadata:   ent.Metadata,
	}
}
