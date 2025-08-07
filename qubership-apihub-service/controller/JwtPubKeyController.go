// Copyright 2024-2025 NetCracker Technology Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controller

import (
	"net/http"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/exception"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/security"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/utils"
)

type JwtPubKeyController interface {
	GetRsaPublicKey(w http.ResponseWriter, r *http.Request)
}

func NewJwtPubKeyController() JwtPubKeyController {
	return &jwtPubKeyControllerImpl{}
}

type jwtPubKeyControllerImpl struct {
}

func (t jwtPubKeyControllerImpl) GetRsaPublicKey(w http.ResponseWriter, r *http.Request) {
	key := security.GetPublicKey()
	if key == nil {
		utils.RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusNotFound,
			Message: "public key not found",
		})
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	if _, err := w.Write(key); err != nil {
		// Можно логировать ошибку, если log импортирован
	}
}
