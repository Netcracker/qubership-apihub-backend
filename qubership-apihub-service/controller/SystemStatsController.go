package controller

import (
	"net/http"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/context"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/exception"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/responder"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/service"
)

type SystemStatsController interface {
	GetSystemStats(w http.ResponseWriter, r *http.Request)
}

func NewSystemStatsController(statsService service.SystemStatsService, roleService service.RoleService, responder *responder.Responder) SystemStatsController {
	return &systemStatsControllerImpl{
		statsService: statsService,
		roleService:  roleService,
		responder:    responder,
	}
}

type systemStatsControllerImpl struct {
	statsService service.SystemStatsService
	roleService  service.RoleService
	responder    *responder.Responder
}

func (s systemStatsControllerImpl) GetSystemStats(w http.ResponseWriter, r *http.Request) {
	ctx := context.Create(r)
	sufficientPrivileges := s.roleService.IsSysadm(ctx)
	if !sufficientPrivileges {
		s.responder.RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusForbidden,
			Code:    exception.InsufficientPrivileges,
			Message: exception.InsufficientPrivilegesMsg,
		})
		return
	}
	stats, err := s.statsService.GetSystemStats(r.Context())
	if err != nil {
		s.responder.RespondWithError(w, "Failed to get system statistics", err)
		return
	}
	s.responder.RespondWithJson(w, http.StatusOK, stats)
}
