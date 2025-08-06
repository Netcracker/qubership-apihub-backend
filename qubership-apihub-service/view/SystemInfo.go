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

package view

import (
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/security/idp"
)

type SystemInfo struct {
	BackendVersion      string      `json:"backendVersion"`
	ProductionMode      bool        `json:"productionMode"`
	Notification        string      `json:"notification,omitempty"`
	ExternalLinks       []string    `json:"externalLinks"`
	MigrationInProgress bool        `json:"migrationInProgress"`
	LinterEnabled       bool        `json:"linterEnabled"` // TODO: remove, replaced with Extensions
	Extensions          []Extension `json:"extensions"`
}

type SystemConfigurationInfo_deprecated struct {
	SSOIntegrationEnabled bool   `json:"ssoIntegrationEnabled"`
	AutoRedirect          bool   `json:"autoRedirect"`
	DefaultWorkspaceId    string `json:"defaultWorkspaceId"`
}

type SystemConfigurationInfo struct {
	DefaultWorkspaceId string         `json:"defaultWorkspaceId"`
	AuthConfig         idp.AuthConfig `json:"authConfig"`
}

type Extension struct {
	Name       string `json:"name" validate:"required"`
	BaseUrl    string `json:"baseUrl" validate:"required"`
	PathPrefix string `json:"pathPrefix" validate:"required"`
}
