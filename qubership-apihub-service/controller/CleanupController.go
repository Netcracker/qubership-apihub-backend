package controller

import (
	"net/http"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/exception"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/responder"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/service/cleanup"
	log "github.com/sirupsen/logrus"
)

type CleanupController interface {
	ClearTestData(w http.ResponseWriter, r *http.Request)
}

func NewCleanupController(cleanupService cleanup.CleanupService, responder *responder.Responder) CleanupController {
	return &cleanupControllerImpl{
		cleanupService: cleanupService,
		responder:      responder,
	}
}

type cleanupControllerImpl struct {
	cleanupService cleanup.CleanupService
	responder      *responder.Responder
}

func (c cleanupControllerImpl) ClearTestData(w http.ResponseWriter, r *http.Request) {
	testId, err := getUnescapedStringParam(r, "testId")
	if err != nil {
		c.responder.RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusBadRequest,
			Code:    exception.InvalidURLEscape,
			Message: exception.InvalidURLEscapeMsg,
			Params:  map[string]interface{}{"param": "testId"},
			Debug:   err.Error(),
		})
		return
	}
	err = c.cleanupService.ClearTestData(testId)
	if err != nil {
		log.Error("Failed to clear test data: ", err.Error())
		if customError, ok := err.(*exception.CustomError); ok {
			c.responder.RespondWithCustomError(w, customError)
		} else {
			c.responder.RespondWithCustomError(w, &exception.CustomError{
				Status:  http.StatusInternalServerError,
				Message: "Failed to clear test data",
				Debug:   err.Error()})
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
