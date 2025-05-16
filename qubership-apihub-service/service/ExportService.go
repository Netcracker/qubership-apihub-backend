package service

import (
	"fmt"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/archive"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/context"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/entity"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/exception"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/repository"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/service/validation"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
	log "github.com/sirupsen/logrus"
	"net/http"
	"time"
)

type ExportService interface {
	StartVersionExport(ctx context.SecurityContext, req view.ExportVersionReq) (string, error)
	StartOASDocExport(ctx context.SecurityContext, req view.ExportOASDocumentReq) (string, error)
	StartRESTOpGroupExport(ctx context.SecurityContext, req view.ExportRestOperationsGroupReq) (string, error)

	GetAsyncExportStatus(exportId string) (*view.ExportStatus, *view.ExportResult, error)

	StartCleanupOldResultsJob()

	PublishTransformedDocuments(buildArc *archive.BuildResultArchive, publishId string) error // deprecated
	StoreExportResult(userId string, exportId string, buildResult []byte, fileName string, buildConfig view.BuildConfig) error
}

func NewExportService(exportRepository repository.ExportResultRepository, portalService PortalService, buildService BuildService, packageExportConfigService PackageExportConfigService) ExportService {
	return &exportServiceImpl{
		exportRepository:           exportRepository,
		packageExportConfigService: packageExportConfigService,
		portalService:              portalService,
		buildService:               buildService,
		tempHtmlCache:              make(map[string][]byte),
		tempHtmlFNameCache:         make(map[string]string),
		tempErrCache:               make(map[string]error),
	}
}

type exportServiceImpl struct {
	exportRepository repository.ExportResultRepository

	packageExportConfigService PackageExportConfigService
	// FIXME: to be removed!!!
	portalService      PortalService
	buildService       BuildService
	tempHtmlCache      map[string][]byte
	tempHtmlFNameCache map[string]string
	tempErrCache       map[string]error
}

func (e exportServiceImpl) StoreExportResult(userId string, exportId string, buildResult []byte, fileName string, buildConfig view.BuildConfig) error {
	ent := entity.ExportResultEntity{
		ExportId:  exportId,
		Config:    buildConfig,
		CreatedAt: time.Now(),
		CreatedBy: userId,
		Data:      buildResult,
		Filename:  fileName,
	}
	err := e.exportRepository.SaveExportResult(ent)
	return err
}

func (e exportServiceImpl) StartVersionExport(ctx context.SecurityContext, req view.ExportVersionReq) (string, error) {
	// TODO: check package and version exists
	// TODO: validate 	req.Format

	var allowedOasExtensions *[]string
	var err error

	if req.RemoveOasExtensions {
		allowedOasExtensions, err = e.makeAllowedOasExtensions(req.PackageID)
		if err != nil {
			return "", fmt.Errorf("failed to make allowed oas extensions: %w", err)
		}
	}

	user := ctx.GetUserId()
	if user == "" {
		user = ctx.GetApiKeyId()
	}

	config := view.BuildConfig{
		PackageId: req.PackageID,
		Version:   req.Version,
		BuildType: view.ExportVersion,
		Format:    req.Format,
		CreatedBy: user,
		//ValidationRulesSeverity: view.ValidationRulesSeverity{},
		AllowedOasExtensions: allowedOasExtensions,
	}

	buildId, config, err := e.buildService.CreateBuildWithoutDependencies(config, false, "")
	if err != nil {
		return "", fmt.Errorf("failed to create build %s: %w", req.PackageID, err)
	}
	return buildId, nil
}

func (e exportServiceImpl) StartOASDocExport(ctx context.SecurityContext, req view.ExportOASDocumentReq) (string, error) {
	// TODO: check package and version exists
	// TODO: validate 	req.Format

	var allowedOasExtensions *[]string
	var err error

	if req.RemoveOasExtensions {
		allowedOasExtensions, err = e.makeAllowedOasExtensions(req.PackageID)
		if err != nil {
			return "", fmt.Errorf("failed to make allowed oas extensions: %w", err)
		}
	}

	user := ctx.GetUserId()
	if user == "" {
		user = ctx.GetApiKeyId()
	}

	config := view.BuildConfig{
		PackageId:  req.PackageID,
		Version:    req.Version,
		DocumentId: req.DocumentID,
		BuildType:  view.ExportRestDocument,
		Format:     req.Format,
		CreatedBy:  user,
		//ValidationRulesSeverity: view.ValidationRulesSeverity{},
		AllowedOasExtensions: allowedOasExtensions,
	}

	buildId, config, err := e.buildService.CreateBuildWithoutDependencies(config, false, "")
	if err != nil {
		return "", fmt.Errorf("failed to create build %s: %w", req.PackageID, err)
	}
	return buildId, nil
}

func (e exportServiceImpl) StartRESTOpGroupExport(ctx context.SecurityContext, req view.ExportRestOperationsGroupReq) (string, error) {
	// TODO: check package and version exists
	// TODO: validate enums,etc

	// FIXME: temporary implementation!!!

	var allowedOasExtensions *[]string
	var err error

	if req.RemoveOasExtensions {
		allowedOasExtensions, err = e.makeAllowedOasExtensions(req.PackageID)
		if err != nil {
			return "", fmt.Errorf("failed to make allowed oas extensions: %w", err)
		}
	}

	buildConfig := view.BuildConfig{
		PackageId: req.PackageID,
		Version:   req.Version,
		BuildType: view.ExportRestOperationsGroup,
		CreatedBy: ctx.GetUserId(),
		ApiType:   string(view.RestApiType),
		GroupName: req.GroupName,

		//ValidationRulesSeverity: view.ValidationRulesSeverity{},
		AllowedOasExtensions: allowedOasExtensions,
	}

	exportId, _, err := e.buildService.CreateBuildWithoutDependencies(buildConfig, false, "")
	if err != nil {
		return "", err
	}

	return exportId, nil
}

func (e exportServiceImpl) makeAllowedOasExtensions(packageId string) (*[]string, error) {
	var allowedOasExtensions *[]string

	// TODO: need to test output json!!
	config, err := e.packageExportConfigService.GetConfig(packageId)
	if err != nil {
		return nil, fmt.Errorf("failed to get package %s config: %w", packageId, err)
	}
	var aos []string
	for _, entry := range config.AllowedOasExtensions {
		aos = append(aos, entry.OasExtension)
	}
	allowedOasExtensions = &aos

	return allowedOasExtensions, nil
}

func (e exportServiceImpl) GetAsyncExportStatus(exportId string) (*view.ExportStatus, *view.ExportResult, error) {
	build, err := e.buildService.GetBuild(exportId)
	if err != nil {
		return nil, nil, err
	}
	if build == nil {
		return nil, nil, &exception.CustomError{
			Status:  http.StatusNotFound,
			Code:    exception.ExportProcessNotFound,
			Message: exception.ExportProcessNotFoundMsg,
			Params:  map[string]interface{}{"exportId": exportId},
		}
	}

	switch view.BuildStatusEnum(build.Status) {
	case view.StatusNotStarted, view.StatusRunning:
		return &view.ExportStatus{
			Status: build.Status,
		}, nil, err
	case view.StatusComplete:
		break
	case view.StatusError:
		return &view.ExportStatus{
			Status:  build.Status,
			Message: &build.Details,
		}, nil, nil
	default:
		return nil, nil, fmt.Errorf("unknown export status %s", build.Status)
	}

	// processing complete status
	resultEnt, err := e.exportRepository.GetExportResult(exportId)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get export result %s: %w", exportId, err)
	}
	if resultEnt == nil {
		// most probably export result was already cleaned up
		return nil, nil, nil
	}

	return nil, &view.ExportResult{Data: resultEnt.Data, FileName: resultEnt.Filename}, nil
}

func (e exportServiceImpl) StartCleanupOldResultsJob() {
	cleanupTime := time.Minute * 10 // TODO: configure TTL?

	ticker := time.NewTicker(cleanupTime)
	for range ticker.C {
		err := e.exportRepository.CleanupExportResults(cleanupTime)
		if err != nil {
			log.Warnf("Failed to run export result cleanup job: %s", err.Error())
		} else {
			log.Tracef("Export result cleanup job finished successfully")
		}
	}
}

// deprecated
func (e exportServiceImpl) PublishTransformedDocuments(buildArc *archive.BuildResultArchive, publishId string) error {
	var err error
	if err = buildArc.ReadPackageDocuments(true); err != nil {
		return err
	}
	if err = validation.ValidatePublishBuildResult(buildArc); err != nil {
		return err
	}
	buildArc.PackageInfo.Version, buildArc.PackageInfo.Revision, err = SplitVersionRevision(buildArc.PackageInfo.Version)
	if err != nil {
		return err
	}

	buildArcEntitiesReader := archive.NewBuildResultToEntitiesReader(buildArc)
	transformedDocumentsEntity, err := buildArcEntitiesReader.ReadTransformedDocumentsToEntity()
	if err != nil {
		return err
	}
	return e.exportRepository.SaveTransformedDocument(transformedDocumentsEntity, publishId)
}
