package service

import (
	"fmt"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/context"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/utils"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
	"github.com/google/uuid"
)

type ExportService interface {
	StartVersionExport(ctx context.SecurityContext, req view.ExportVersionReq) (string, error)
	StartOASDocExport(ctx context.SecurityContext, req view.ExportOASDocumentReq) (string, error)
	StartRESTOpGroupExport(ctx context.SecurityContext, req view.ExportRestOperationsGroupReq) (string, error)

	GetAsyncExportStatus(exportId string) (*view.ExportStatus, *view.ExportResult, error)
}

func NewExportService(portalService PortalService, buildService BuildService) ExportService {
	return &exportServiceImpl{
		portalService:      portalService,
		buildService:       buildService,
		tempHtmlCache:      make(map[string][]byte),
		tempHtmlFNameCache: make(map[string]string),
		tempErrCache:       make(map[string]error),
	}
}

type exportServiceImpl struct {

	// FIXME: to be removed!!!
	portalService      PortalService
	buildService       BuildService
	tempHtmlCache      map[string][]byte
	tempHtmlFNameCache map[string]string
	tempErrCache       map[string]error
}

func (e exportServiceImpl) StartVersionExport(ctx context.SecurityContext, req view.ExportVersionReq) (string, error) {
	// TODO: check package and version exists
	// TODO: validate 	req.Format

	// FIXME: temporary implementation!!!
	buildId := uuid.NewString()
	utils.SafeAsync(func() {
		data, filename, err := e.portalService.GenerateInteractivePageForPublishedVersion(req.PackageID, req.Version)
		if err != nil {
			e.tempErrCache[buildId] = err
			return
		}
		e.tempHtmlCache[buildId] = data
		e.tempHtmlFNameCache[buildId] = filename

	})
	return buildId, nil
}

func (e exportServiceImpl) StartOASDocExport(ctx context.SecurityContext, req view.ExportOASDocumentReq) (string, error) {
	// TODO: check package and version exists
	// TODO: validate 	req.Format

	// FIXME: temporary implementation!!!
	buildId := uuid.NewString()
	utils.SafeAsync(func() {
		data, filename, err := e.portalService.GenerateInteractivePageForPublishedFile(req.PackageID, req.Version, req.DocumentID)
		if err != nil {
			e.tempErrCache[buildId] = err
			return
		}
		e.tempHtmlCache[buildId] = data
		e.tempHtmlFNameCache[buildId] = filename
	})
	return buildId, nil
}

func (e exportServiceImpl) StartRESTOpGroupExport(ctx context.SecurityContext, req view.ExportRestOperationsGroupReq) (string, error) {
	// TODO: check package and version exists
	// TODO: validate enums,etc

	// FIXME: temporary implementation!!!
	buildConfig := view.BuildConfig{
		PackageId: req.PackageID,
		Version:   req.Version,
		BuildType: view.DocumentGroupType_deprecated,
		CreatedBy: ctx.GetUserId(),
		ApiType:   string(view.RestApiType),
		GroupName: req.GroupName,
	}

	exportId, _, err := e.buildService.CreateBuildWithoutDependencies(buildConfig, false, "")
	if err != nil {
		return "", err
	}

	return exportId, nil
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
