package service

import (
	"encoding/json"
	"net/http"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/entity"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/exception"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/repository"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
)

type MCPContractService interface {
	ListMcpEntities(packageId, versionName, kind, textFilter string, limit, offset int) (*view.McpEntityListView, error)
	GetMcpEntity(packageId, versionName, mcpEntityId string) (interface{}, error)
	GetVersionSummary(packageId, versionName string) (*view.VersionMCPContractsSummary, error)
}

func NewMCPContractService(mcpRepo repository.MCPContractRepository, publishedRepo repository.PublishedRepository) MCPContractService {
	return &mcpContractServiceImpl{mcpRepo: mcpRepo, publishedRepo: publishedRepo}
}

type mcpContractServiceImpl struct {
	mcpRepo       repository.MCPContractRepository
	publishedRepo repository.PublishedRepository
}

func (s *mcpContractServiceImpl) resolveRevision(packageId, versionName string) (string, int, error) {
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

func (s *mcpContractServiceImpl) ListMcpEntities(packageId, versionName, kind, textFilter string, limit, offset int) (*view.McpEntityListView, error) {
	version, revision, err := s.resolveRevision(packageId, versionName)
	if err != nil {
		return nil, err
	}
	entities, err := s.mcpRepo.ListMcpEntities(packageId, version, revision, kind, textFilter, limit, offset)
	if err != nil {
		return nil, err
	}
	result := &view.McpEntityListView{Entities: make([]interface{}, 0, len(entities))}
	for _, ent := range entities {
		result.Entities = append(result.Entities, makeMcpEntityView(ent, packageId, version, revision))
	}
	return result, nil
}

func (s *mcpContractServiceImpl) GetMcpEntity(packageId, versionName, mcpEntityId string) (interface{}, error) {
	version, revision, err := s.resolveRevision(packageId, versionName)
	if err != nil {
		return nil, err
	}
	ent, data, err := s.mcpRepo.GetMcpEntity(packageId, version, revision, mcpEntityId)
	if err != nil {
		return nil, err
	}
	if ent == nil {
		return nil, &exception.CustomError{
			Status:  http.StatusNotFound,
			Code:    exception.PublishedVersionNotFound,
			Message: "MCP entity not found",
			Params:  map[string]interface{}{"mcpEntityId": mcpEntityId},
		}
	}
	detail := view.McpEntityDetailView{McpEntityView: *makeMcpEntityView(ent, packageId, version, revision)}
	if len(data) > 0 {
		var parsed interface{}
		if err := json.Unmarshal(data, &parsed); err == nil {
			detail.Data = parsed
		}
	}
	return detail, nil
}

func (s *mcpContractServiceImpl) GetVersionSummary(packageId, versionName string) (*view.VersionMCPContractsSummary, error) {
	version, revision, err := s.resolveRevision(packageId, versionName)
	if err != nil {
		return nil, err
	}
	counts, err := s.mcpRepo.GetEntitiesCount(packageId, version, revision)
	if err != nil {
		return nil, err
	}
	if len(counts) == 0 {
		return nil, nil
	}
	summary := &view.VersionMCPContractsSummary{}
	for _, c := range counts {
		switch c.Kind {
		case view.McpKindInit:
			summary.Init = c.Count
		case view.McpKindTool:
			summary.Tools = c.Count
		case view.McpKindPrompt:
			summary.Prompts = c.Count
		case view.McpKindResource:
			summary.Resources = c.Count
		}
	}
	return summary, nil
}

func makeMcpEntityView(ent *entity.MCPContractEntity, packageId, version string, revision int) *view.McpEntityView {
	return &view.McpEntityView{
		EntityId:    ent.McpEntityId,
		Kind:        ent.Kind,
		Name:        ent.Name,
		McpEndpoint: ent.McpEndpoint,
		DocumentId:  ent.DocumentId,
		PackageRef:  view.MakePackageRefKey(packageId, version, revision),
		Metadata:    ent.Metadata,
	}
}
