package service

import (
	"fmt"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/context"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
)

type ExportService interface {
	StartVersionExport(ctx context.SecurityContext, req view.ExportVersionReq) (string, error)
	StartOASDocExport(ctx context.SecurityContext, req view.ExportOASDocumentReq) (string, error)
	StartRESTOpGroupExport(ctx context.SecurityContext, req view.ExportRestOperationsGroupReq) (string, error)

	GetAsyncExportStatus(exportId string) (*view.ExportStatus, *view.ExportResult, error)

	CleanupOldResults() error
}

func NewExportService(portalService PortalService, buildService BuildService, packageExportConfigService PackageExportConfigService) ExportService {
	return &exportServiceImpl{
		packageExportConfigService: packageExportConfigService,
		portalService:              portalService,
		buildService:               buildService,
		tempHtmlCache:              make(map[string][]byte),
		tempHtmlFNameCache:         make(map[string]string),
		tempErrCache:               make(map[string]error),
	}
}

type exportServiceImpl struct {
	packageExportConfigService PackageExportConfigService
	// FIXME: to be removed!!!
	portalService      PortalService
	buildService       BuildService
	tempHtmlCache      map[string][]byte
	tempHtmlFNameCache map[string]string
	tempErrCache       map[string]error
}

// TODO: use in job
func (e exportServiceImpl) CleanupOldResults() error {
	// TODO: how to configure TTL? via config?

	//TODO implement me
	panic("implement me")
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

	// FIXME: temporary implementation!!!
	/*buildId := uuid.NewString()
	e.tempHtmlFNameCache[buildId] = "running"
	utils.SafeAsync(func() {
		time.Sleep(time.Second * 10)
		data, filename, err := e.portalService.GenerateInteractivePageForPublishedVersion(req.PackageID, req.Version)
		if err != nil {
			e.tempErrCache[buildId] = err
			return
		}
		e.tempHtmlCache[buildId] = data
		e.tempHtmlFNameCache[buildId] = filename
	})
	return buildId, nil*/
}

func (e exportServiceImpl) StartOASDocExport(ctx context.SecurityContext, req view.ExportOASDocumentReq) (string, error) {
	// TODO: check package and version exists
	// TODO: validate 	req.Format

	// FIXME: temporary implementation!!!
	/*buildId := uuid.NewString()
	e.tempHtmlFNameCache[buildId] = "running"
	utils.SafeAsync(func() {
		time.Sleep(time.Second * 10)
		data, filename, err := e.portalService.GenerateInteractivePageForPublishedFile(req.PackageID, req.Version, req.DocumentID)
		if err != nil {
			e.tempErrCache[buildId] = err
			return
		}
		e.tempHtmlCache[buildId] = data
		e.tempHtmlFNameCache[buildId] = filename
	})
	return buildId, nil*/

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
		// FIXME: temp code!
		// check maps
		eerr := e.tempErrCache[exportId]
		if eerr != nil {
			str := eerr.Error()
			return &view.ExportStatus{
				Status:  string(view.StatusError),
				Message: &str,
			}, nil, nil
		}
		/////
		isRunning := e.tempHtmlFNameCache[exportId]
		if isRunning == "running" {
			return &view.ExportStatus{
				Status:  string(view.StatusRunning),
				Message: nil,
			}, nil, nil
		}

		//////

		if res, ok := e.tempHtmlCache[exportId]; ok {
			fName := e.tempHtmlFNameCache[exportId]
			if fName == "" {
				fName = "file"
			}
			return nil, &view.ExportResult{Data: res, FileName: fName}, nil
		}
		return nil, nil, nil
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

	data := "stub data here"
	return nil, &view.ExportResult{Data: []byte(data), FileName: "success.file"}, nil

}
