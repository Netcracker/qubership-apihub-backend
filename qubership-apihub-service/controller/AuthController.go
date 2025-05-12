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
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/security/idp"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/service"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/utils"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
	"net/http"
)

type AuthController interface {
	SAMLAssertionConsumerHandler(w http.ResponseWriter, r *http.Request)
	StartAuthentication(w http.ResponseWriter, r *http.Request)
	ServeMetadata(w http.ResponseWriter, r *http.Request)
	OIDCCallbackHandler(w http.ResponseWriter, r *http.Request)
	GetSystemInfo(w http.ResponseWriter, r *http.Request)
}

func NewAuthController(userService service.UserService, systemInfoService service.SystemInfoService, idpManager idp.Manager) AuthController {
	return &authControllerImpl{
		idpManager:        idpManager,
		userService:       userService,
		systemInfoService: systemInfoService,
	}
}

type authControllerImpl struct {
	idpManager        idp.Manager
	userService       service.UserService
	systemInfoService service.SystemInfoService
}

func (a *authControllerImpl) ServeMetadata(w http.ResponseWriter, r *http.Request) {
	idpId := getStringParam(r, "idpId")
	provider, exists := a.idpManager.GetProvider(idpId)
	if exists {
		provider.ServeMetadata(w, r)
	} else {
		//TODO: throw custom error
	}
}

// StartAuthentication Frontend calls this endpoint to SSO login user via SAML
func (a *authControllerImpl) StartAuthentication(w http.ResponseWriter, r *http.Request) {
	idpId := getStringParam(r, "idpId")
	provider, exists := a.idpManager.GetProvider(idpId)
	if exists {
		provider.StartAuthentication(w, r)
	} else {
		//TODO: throw custom error
	}
}

// AssertionConsumerHandler This endpoint is called by ADFS when auth procedure is complete on it's side. ADFS posts the response here.
func (a *authControllerImpl) SAMLAssertionConsumerHandler(w http.ResponseWriter, r *http.Request) {
	idpId := getStringParam(r, "idpId")
	provider, exists := a.idpManager.GetProvider(idpId)
	if exists {
		provider.CallbackHandler(w, r)
	} else {
		//TODO: throw custom error
	}
}

func (a *authControllerImpl) OIDCCallbackHandler(w http.ResponseWriter, r *http.Request) {
	idpId := getStringParam(r, "idpId")
	provider, exists := a.idpManager.GetProvider(idpId)
	if exists {
		provider.CallbackHandler(w, r)
	} else {
		//TODO: throw custom error
	}
}

func (a *authControllerImpl) GetSystemInfo(w http.ResponseWriter, r *http.Request) {
	ssoIntegrationEnabled := a.idpManager.IsSSOIntegrationEnabled()
	utils.RespondWithJson(w, http.StatusOK,
		view.SystemConfigurationInfo{
			SSOIntegrationEnabled: ssoIntegrationEnabled,
			AutoRedirect:          ssoIntegrationEnabled,
			DefaultWorkspaceId:    a.systemInfoService.GetDefaultWorkspaceId(),
			AuthConfig:            a.systemInfoService.GetAuthConfig(),
		})
}
