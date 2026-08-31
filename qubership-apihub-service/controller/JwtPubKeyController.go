package controller

import (
	"net/http"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/exception"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/responder"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/security"
)

type JwtPubKeyController interface {
	GetRsaPublicKey(w http.ResponseWriter, r *http.Request)
}

func NewJwtPubKeyController(responder *responder.Responder, authHandler *security.AuthHandler) JwtPubKeyController {
	return &jwtPubKeyControllerImpl{responder: responder, authHandler: authHandler}
}

type jwtPubKeyControllerImpl struct {
	responder   *responder.Responder
	authHandler *security.AuthHandler
}

func (t jwtPubKeyControllerImpl) GetRsaPublicKey(w http.ResponseWriter, r *http.Request) {
	key := t.authHandler.GetPublicKey()
	if key == nil {
		t.responder.RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusNotFound,
			Message: "public key not found",
		})
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(key)
}
