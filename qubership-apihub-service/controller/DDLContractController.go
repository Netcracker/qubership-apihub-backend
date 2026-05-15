package controller

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/context"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/exception"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/service"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/utils"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
)

type DDLContractController interface {
	ListDdlTables(w http.ResponseWriter, r *http.Request)
	GetDdlTable(w http.ResponseWriter, r *http.Request)
	GetDdlTableChanges(w http.ResponseWriter, r *http.Request)
}

func NewDDLContractController(roleService service.RoleService,
	ddlService service.DDLContractService,
	ptHandler service.PackageTransitionHandler) DDLContractController {
	return &ddlContractControllerImpl{
		roleService: roleService,
		ddlService:  ddlService,
		ptHandler:   ptHandler,
	}
}

type ddlContractControllerImpl struct {
	roleService service.RoleService
	ddlService  service.DDLContractService
	ptHandler   service.PackageTransitionHandler
}

func (c *ddlContractControllerImpl) checkReadAccess(w http.ResponseWriter, r *http.Request, packageId string) bool {
	ctx := context.Create(r)
	ok, err := c.roleService.HasRequiredPermissions(ctx, packageId, view.ReadPermission)
	if err != nil {
		handlePkgRedirectOrRespondWithError(w, r, c.ptHandler, packageId, "Failed to check user privileges", err)
		return false
	}
	if !ok {
		utils.RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusForbidden,
			Code:    exception.InsufficientPrivileges,
			Message: exception.InsufficientPrivilegesMsg,
		})
		return false
	}
	return true
}

func (c *ddlContractControllerImpl) ListDdlTables(w http.ResponseWriter, r *http.Request) {
	packageId := getStringParam(r, "packageId")
	if !c.checkReadAccess(w, r, packageId) {
		return
	}
	versionName, err := getUnescapedStringParam(r, "version")
	if err != nil {
		utils.RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusBadRequest,
			Code:    exception.InvalidURLEscape,
			Message: exception.InvalidURLEscapeMsg,
			Params:  map[string]interface{}{"param": "version"},
			Debug:   err.Error(),
		})
		return
	}
	kind, _ := url.QueryUnescape(r.URL.Query().Get("kind"))
	textFilter, _ := url.QueryUnescape(r.URL.Query().Get("textFilter"))
	limit, limErr := getLimitQueryParam(r)
	if limErr != nil {
		utils.RespondWithCustomError(w, limErr)
		return
	}
	offset := 0
	if r.URL.Query().Get("offset") != "" {
		offset, _ = strconv.Atoi(r.URL.Query().Get("offset"))
	}
	result, svcErr := c.ddlService.ListDdlTables(packageId, versionName, kind, textFilter, limit, offset)
	if svcErr != nil {
		handlePkgRedirectOrRespondWithError(w, r, c.ptHandler, packageId, "Failed to list DDL tables", svcErr)
		return
	}
	utils.RespondWithJson(w, http.StatusOK, result)
}

func (c *ddlContractControllerImpl) GetDdlTable(w http.ResponseWriter, r *http.Request) {
	packageId := getStringParam(r, "packageId")
	if !c.checkReadAccess(w, r, packageId) {
		return
	}
	versionName, err := getUnescapedStringParam(r, "version")
	if err != nil {
		utils.RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusBadRequest,
			Code:    exception.InvalidURLEscape,
			Message: exception.InvalidURLEscapeMsg,
			Params:  map[string]interface{}{"param": "version"},
			Debug:   err.Error(),
		})
		return
	}
	tableId, err := getUnescapedStringParam(r, "tableId")
	if err != nil {
		utils.RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusBadRequest,
			Code:    exception.InvalidURLEscape,
			Message: exception.InvalidURLEscapeMsg,
			Params:  map[string]interface{}{"param": "tableId"},
			Debug:   err.Error(),
		})
		return
	}
	result, svcErr := c.ddlService.GetDdlTable(packageId, versionName, tableId)
	if svcErr != nil {
		handlePkgRedirectOrRespondWithError(w, r, c.ptHandler, packageId, "Failed to get DDL table", svcErr)
		return
	}
	utils.RespondWithJson(w, http.StatusOK, result)
}

func (c *ddlContractControllerImpl) GetDdlTableChanges(w http.ResponseWriter, r *http.Request) {
	packageId := getStringParam(r, "packageId")
	if !c.checkReadAccess(w, r, packageId) {
		return
	}
	versionName, err := getUnescapedStringParam(r, "version")
	if err != nil {
		utils.RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusBadRequest,
			Code:    exception.InvalidURLEscape,
			Message: exception.InvalidURLEscapeMsg,
			Params:  map[string]interface{}{"param": "version"},
			Debug:   err.Error(),
		})
		return
	}
	tableId, err := getUnescapedStringParam(r, "tableId")
	if err != nil {
		utils.RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusBadRequest,
			Code:    exception.InvalidURLEscape,
			Message: exception.InvalidURLEscapeMsg,
			Params:  map[string]interface{}{"param": "tableId"},
			Debug:   err.Error(),
		})
		return
	}
	result, svcErr := c.ddlService.GetDdlTableChanges(packageId, versionName, tableId)
	if svcErr != nil {
		handlePkgRedirectOrRespondWithError(w, r, c.ptHandler, packageId, "Failed to get DDL table changes", svcErr)
		return
	}
	utils.RespondWithJson(w, http.StatusOK, result)
}
