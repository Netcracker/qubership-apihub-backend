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
	"crypto/tls"
	"io"
	"net/http"
	"net/url"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/utils"
	log "github.com/sirupsen/logrus"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/exception"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/service"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
)

type ProxyController interface {
	Proxy(w http.ResponseWriter, req *http.Request)
}

func NewAgentProxyController(agentRegistrationService service.AgentRegistrationService, systemInfoService service.SystemInfoService) ProxyController {
	return &agentProxyControllerImpl{
		agentRegistrationService: agentRegistrationService,
		tr:                       http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
		systemInfoService:        systemInfoService,
	}
}

type agentProxyControllerImpl struct {
	agentRegistrationService service.AgentRegistrationService
	tr                       http.Transport
	systemInfoService        service.SystemInfoService
}

func (a *agentProxyControllerImpl) Proxy(w http.ResponseWriter, r *http.Request) {
	agentId := getStringParam(r, "agentId")

	agent, err := a.agentRegistrationService.GetAgent(agentId)
	if err != nil {
		utils.RespondWithError(w, "Failed to proxy a request", err)
		return
	}
	if agent == nil {
		utils.RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusNotFound,
			Code:    exception.AgentNotFound,
			Message: exception.AgentNotFoundMsg,
			Params:  map[string]interface{}{"agentId": agentId},
		})
		return
	}
	if agent.Status != view.AgentStatusActive {
		utils.RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusFailedDependency,
			Code:    exception.InactiveAgent,
			Message: exception.InactiveAgentMsg,
			Params:  map[string]interface{}{"agentId": agentId}})
		return
	}
	if agent.AgentVersion == "" {
		utils.RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusFailedDependency,
			Code:    exception.IncompatibleAgentVersion,
			Message: exception.IncompatibleAgentVersionMsg,
			Params:  map[string]interface{}{"version": agent.AgentVersion},
		})
	}
	if agent.CompatibilityError != nil && agent.CompatibilityError.Severity == view.SeverityError {
		utils.RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusFailedDependency,
			Message: agent.CompatibilityError.Message,
		})
	}
	agentUrl, err := url.Parse(agent.AgentUrl)
	if err != nil {
		utils.RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusFailedDependency,
			Code:    exception.InvalidAgentUrl,
			Message: exception.InvalidAgentUrlMsg,
			Params:  map[string]interface{}{"url": agent.AgentUrl, "agentId": agentId},
			Debug:   err.Error(),
		})
		return
	}
	if err := utils.IsHostValid(agentUrl, a.systemInfoService.GetAllowedHosts()); err != nil {
		utils.RespondWithCustomError(w, err)
		return
	}

	r.URL.Host = agentUrl.Host
	r.URL.Scheme = agentUrl.Scheme
	r.Host = agentUrl.Host
	log.Debugf("Sending proxy request to %s", r.URL)
	resp, err := a.tr.RoundTrip(r)
	if err != nil {
		utils.RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusFailedDependency,
			Code:    exception.ProxyFailed,
			Message: exception.ProxyFailedMsg,
			Params:  map[string]interface{}{"url": r.URL.String()},
			Debug:   err.Error(),
		})
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if err := copyHeader(w.Header(), resp.Header); err != nil {
		utils.RespondWithCustomError(w, err)
		return
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}
