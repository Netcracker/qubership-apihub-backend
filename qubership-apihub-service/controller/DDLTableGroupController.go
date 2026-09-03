package controller

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/exception"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/secctx"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/service"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/utils"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
)

type DDLTableGroupController interface {
	ListDdlTableGroups(w http.ResponseWriter, r *http.Request)
	GetGroupedDdlEntities(w http.ResponseWriter, r *http.Request)
	CreateDdlTableGroup(w http.ResponseWriter, r *http.Request)
	UpdateDdlTableGroup(w http.ResponseWriter, r *http.Request)
	DeleteDdlTableGroup(w http.ResponseWriter, r *http.Request)
}

func NewDDLTableGroupController(roleService service.RoleService,
	ddlTableGroupService service.DDLTableGroupService,
	versionService service.VersionService,
	ptHandler service.PackageTransitionHandler) DDLTableGroupController {
	return &ddlTableGroupControllerImpl{
		roleService:          roleService,
		ddlTableGroupService: ddlTableGroupService,
		versionService:       versionService,
		ptHandler:            ptHandler,
	}
}

type ddlTableGroupControllerImpl struct {
	roleService          service.RoleService
	ddlTableGroupService service.DDLTableGroupService
	versionService       service.VersionService
	ptHandler            service.PackageTransitionHandler
}

func (c *ddlTableGroupControllerImpl) checkReadAccess(w http.ResponseWriter, r *http.Request, ctx context.Context, packageId string) bool {
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

func (c *ddlTableGroupControllerImpl) checkManageVersionAccess(w http.ResponseWriter, r *http.Request, ctx context.Context, packageId, versionName string) bool {
	versionStatus, err := c.versionService.GetVersionStatus(ctx, packageId, versionName)
	if err != nil {
		handlePkgRedirectOrRespondWithError(w, r, c.ptHandler, packageId, "Failed to check user privileges", err)
		return false
	}
	ok, err := c.roleService.HasManageVersionPermission(ctx, packageId, versionStatus)
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

func getVersionPathParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	return getUnescapedPathParam(w, r, "version")
}

func getUnescapedPathParam(w http.ResponseWriter, r *http.Request, param string) (string, bool) {
	value, err := getUnescapedStringParam(r, param)
	if err != nil {
		utils.RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusBadRequest,
			Code:    exception.InvalidURLEscape,
			Message: exception.InvalidURLEscapeMsg,
			Params:  map[string]interface{}{"param": param},
			Debug:   err.Error(),
		})
		return "", false
	}
	return value, true
}

func readJsonRequestBody(w http.ResponseWriter, r *http.Request, req interface{}) bool {
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		utils.RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusBadRequest,
			Code:    exception.BadRequestBody,
			Message: exception.BadRequestBodyMsg,
			Debug:   err.Error(),
		})
		return false
	}
	if err = json.Unmarshal(body, req); err != nil {
		utils.RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusBadRequest,
			Code:    exception.BadRequestBody,
			Message: exception.BadRequestBodyMsg,
			Debug:   err.Error(),
		})
		return false
	}
	if validationErr := utils.ValidateObject(req); validationErr != nil {
		var customError *exception.CustomError
		if errors.As(validationErr, &customError) {
			utils.RespondWithCustomError(w, customError)
			return false
		}
		utils.RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusBadRequest,
			Code:    exception.BadRequestBody,
			Message: exception.BadRequestBodyMsg,
			Debug:   validationErr.Error(),
		})
		return false
	}
	return true
}

func (c *ddlTableGroupControllerImpl) ListDdlTableGroups(w http.ResponseWriter, r *http.Request) {
	ctx := secctx.MakeUserContext(r)
	packageId := getStringParam(r, "packageId")
	if !c.checkReadAccess(w, r, ctx, packageId) {
		return
	}
	versionName, ok := getVersionPathParam(w, r)
	if !ok {
		return
	}
	result, err := c.ddlTableGroupService.ListDdlTableGroups(ctx, packageId, versionName)
	if err != nil {
		handlePkgRedirectOrRespondWithError(w, r, c.ptHandler, packageId, "Failed to list DDL table groups", err)
		return
	}
	utils.RespondWithJson(w, http.StatusOK, result)
}

func (c *ddlTableGroupControllerImpl) GetGroupedDdlEntities(w http.ResponseWriter, r *http.Request) {
	ctx := secctx.MakeUserContext(r)
	packageId := getStringParam(r, "packageId")
	if !c.checkReadAccess(w, r, ctx, packageId) {
		return
	}
	versionName, ok := getVersionPathParam(w, r)
	if !ok {
		return
	}
	groupName, ok := getUnescapedPathParam(w, r, "groupName")
	if !ok {
		return
	}
	textFilter, _ := url.QueryUnescape(r.URL.Query().Get("textFilter"))
	refPackageId := r.URL.Query().Get("refPackageId")
	limit, limErr := getLimitQueryParam(r)
	if limErr != nil {
		utils.RespondWithCustomError(w, limErr)
		return
	}
	offset := 0
	if r.URL.Query().Get("offset") != "" {
		offset, _ = strconv.Atoi(r.URL.Query().Get("offset"))
	}
	result, err := c.ddlTableGroupService.GetGroupedDdlEntities(ctx, packageId, versionName, groupName, refPackageId, textFilter, limit, offset)
	if err != nil {
		handlePkgRedirectOrRespondWithError(w, r, c.ptHandler, packageId, "Failed to get DDL entities of the group", err)
		return
	}
	utils.RespondWithJson(w, http.StatusOK, result)
}

func (c *ddlTableGroupControllerImpl) CreateDdlTableGroup(w http.ResponseWriter, r *http.Request) {
	ctx := secctx.MakeUserContext(r)
	packageId := getStringParam(r, "packageId")
	versionName, ok := getVersionPathParam(w, r)
	if !ok {
		return
	}
	if !c.checkManageVersionAccess(w, r, ctx, packageId, versionName) {
		return
	}
	var req view.CreateDdlTableGroupReq
	if !readJsonRequestBody(w, r, &req) {
		return
	}
	if err := c.ddlTableGroupService.CreateDdlTableGroup(ctx, packageId, versionName, req); err != nil {
		handlePkgRedirectOrRespondWithError(w, r, c.ptHandler, packageId, "Failed to create DDL table group", err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (c *ddlTableGroupControllerImpl) UpdateDdlTableGroup(w http.ResponseWriter, r *http.Request) {
	ctx := secctx.MakeUserContext(r)
	packageId := getStringParam(r, "packageId")
	versionName, ok := getVersionPathParam(w, r)
	if !ok {
		return
	}
	groupName, ok := getUnescapedPathParam(w, r, "groupName")
	if !ok {
		return
	}
	if !c.checkManageVersionAccess(w, r, ctx, packageId, versionName) {
		return
	}
	var req view.UpdateDdlTableGroupReq
	if !readJsonRequestBody(w, r, &req) {
		return
	}
	if err := c.ddlTableGroupService.UpdateDdlTableGroup(ctx, packageId, versionName, groupName, req); err != nil {
		handlePkgRedirectOrRespondWithError(w, r, c.ptHandler, packageId, "Failed to update DDL table group", err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (c *ddlTableGroupControllerImpl) DeleteDdlTableGroup(w http.ResponseWriter, r *http.Request) {
	ctx := secctx.MakeUserContext(r)
	packageId := getStringParam(r, "packageId")
	versionName, ok := getVersionPathParam(w, r)
	if !ok {
		return
	}
	groupName, ok := getUnescapedPathParam(w, r, "groupName")
	if !ok {
		return
	}
	if !c.checkManageVersionAccess(w, r, ctx, packageId, versionName) {
		return
	}
	if err := c.ddlTableGroupService.DeleteDdlTableGroup(ctx, packageId, versionName, groupName); err != nil {
		handlePkgRedirectOrRespondWithError(w, r, c.ptHandler, packageId, "Failed to delete DDL table group", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
