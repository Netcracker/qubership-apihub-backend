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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/service"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/utils"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/client"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/context"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/exception"
)

const (
	maxHeaders      = 100
	maxHeaderValues = 1000
)

type AgentController interface {
	ProcessAgentSignal(w http.ResponseWriter, r *http.Request)
	ListAgents(w http.ResponseWriter, r *http.Request)
	GetAgent(w http.ResponseWriter, r *http.Request)
	GetAgentNamespaces(w http.ResponseWriter, r *http.Request)
	ListServiceNames(w http.ResponseWriter, r *http.Request)
}

func NewAgentController(agentRegistrationService service.AgentRegistrationService, agentClient client.AgentClient, isSysadm func(context.SecurityContext) bool) AgentController {
	return &agentControllerImpl{
		agentRegistrationService: agentRegistrationService,
		agentClient:              agentClient,
		isSysadm:                 isSysadm,
	}
}

type agentControllerImpl struct {
	agentRegistrationService service.AgentRegistrationService
	agentClient              client.AgentClient
	isSysadm                 func(context.SecurityContext) bool
}

func (a agentControllerImpl) ProcessAgentSignal(w http.ResponseWriter, r *http.Request) {
	ctx := context.Create(r)
	sufficientPrivileges := a.isSysadm(ctx)
	if !sufficientPrivileges {
		utils.RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusForbidden,
			Code:    exception.InsufficientPrivileges,
			Message: exception.InsufficientPrivilegesMsg,
		})
		return
	}
	defer func() { _ = r.Body.Close() }()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		utils.RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusBadRequest,
			Code:    exception.BadRequestBody,
			Message: exception.BadRequestBodyMsg,
			Debug:   err.Error(),
		})
		return
	}
	var message view.AgentKeepaliveMessage
	err = json.Unmarshal(body, &message)
	if err != nil {
		utils.RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusBadRequest,
			Code:    exception.BadRequestBody,
			Message: exception.BadRequestBodyMsg,
			Debug:   err.Error(),
		})
		return
	}
	validationErr := utils.ValidateObject(message)
	if validationErr != nil {
		if customError, ok := validationErr.(*exception.CustomError); ok {
			utils.RespondWithCustomError(w, customError)
			return
		}
	}
	version, err := a.agentRegistrationService.ProcessAgentSignal(message)
	if err != nil {
		utils.RespondWithError(w, fmt.Sprintf("Failed to process agent keepalive message %+v", message), err)
		return
	}
	utils.RespondWithJson(w, http.StatusOK, version)
}

func (a agentControllerImpl) ListAgents(w http.ResponseWriter, r *http.Request) {
	onlyActiveStr := r.URL.Query().Get("onlyActive")
	var err error
	onlyActive := true
	if onlyActiveStr != "" {
		onlyActive, err = strconv.ParseBool(onlyActiveStr)
		if err != nil {
			utils.RespondWithCustomError(w, &exception.CustomError{
				Status:  http.StatusBadRequest,
				Code:    exception.IncorrectParamType,
				Message: exception.IncorrectParamTypeMsg,
				Params:  map[string]interface{}{"param": "onlyActive", "type": "boolean"},
				Debug:   err.Error(),
			})
			return
		}
	}

	showIncompatibleStr := r.URL.Query().Get("showIncompatible")
	showIncompatible := false
	if showIncompatibleStr != "" {
		showIncompatible, err = strconv.ParseBool(showIncompatibleStr)
		if err != nil {
			utils.RespondWithCustomError(w, &exception.CustomError{
				Status:  http.StatusBadRequest,
				Code:    exception.IncorrectParamType,
				Message: exception.IncorrectParamTypeMsg,
				Params:  map[string]interface{}{"param": "showIncompatible", "type": "boolean"},
				Debug:   err.Error(),
			})
			return
		}
	}

	result, err := a.agentRegistrationService.ListAgents(onlyActive, showIncompatible)
	if err != nil {
		utils.RespondWithError(w, "Failed to list agents", err)
		return
	}

	utils.RespondWithJson(w, http.StatusOK, result)
}

func (a agentControllerImpl) GetAgent(w http.ResponseWriter, r *http.Request) {
	agentId := getStringParam(r, "id")

	agent, err := a.agentRegistrationService.GetAgent(agentId)
	if err != nil {
		utils.RespondWithError(w, "Failed to get agent", err)
		return
	}
	if agent == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	utils.RespondWithJson(w, http.StatusOK, agent)
}

func (a agentControllerImpl) GetAgentNamespaces(w http.ResponseWriter, r *http.Request) {
	agentId := getStringParam(r, "agentId")

	agent, err := a.agentRegistrationService.GetAgent(agentId)
	if err != nil {
		utils.RespondWithError(w, "Failed to get agent namespaces", err)
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
		return
	}
	if agent.CompatibilityError != nil && agent.CompatibilityError.Severity == view.SeverityError {
		utils.RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusFailedDependency,
			Message: agent.CompatibilityError.Message,
		})
		return
	}
	agentNamespaces, err := a.agentClient.GetNamespaces(context.Create(r), agent.AgentUrl)
	if err != nil {
		utils.RespondWithError(w, "Failed to get agent namespaces", err)
		return
	}
	utils.RespondWithJson(w, http.StatusOK, agentNamespaces)
}

func (a agentControllerImpl) ListServiceNames(w http.ResponseWriter, r *http.Request) {
	agentId := getStringParam(r, "agentId")
	namespace := getStringParam(r, "namespace")

	agent, err := a.agentRegistrationService.GetAgent(agentId)
	if err != nil {
		utils.RespondWithError(w, "Failed to get agent namespaces", err)
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
		return
	}
	if agent.CompatibilityError != nil && agent.CompatibilityError.Severity == view.SeverityError {
		utils.RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusFailedDependency,
			Message: agent.CompatibilityError.Message,
		})
		return
	}

	serviceNames, err := a.agentClient.ListServiceNames(context.Create(r), agent.AgentUrl, namespace)
	if err != nil {
		utils.RespondWithError(w, "Failed to get service names", err)
		return
	}
	utils.RespondWithJson(w, http.StatusOK, serviceNames)
}

func copyHeader(dst, src http.Header) *exception.CustomError {
	//validation was added based on security scan results to avoid resource exhaustion
	if len(src) > maxHeaders {
		return &exception.CustomError{
			Status:  http.StatusBadGateway,
			Code:    exception.HeadersLimitExceeded,
			Message: exception.HeadersLimitExceededMsg,
			Params:  map[string]interface{}{"maxHeaders": maxHeaders},
		}
	}

	for k, vv := range src {
		//validation was added based on security scan results to avoid resource exhaustion
		if len(vv) > maxHeaderValues {
			return &exception.CustomError{
				Status:  http.StatusBadGateway,
				Code:    exception.HeaderValuesLimitExceeded,
				Message: exception.HeaderValuesLimitExceededMsg,
				Params:  map[string]interface{}{"key": k, "maxValues": maxHeaderValues},
			}
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
	return nil
}
