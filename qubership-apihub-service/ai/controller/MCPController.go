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

	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/ai/tools"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/service"
)

func InitMCPController(operationService service.OperationService, packageService service.PackageService) (http.Handler, error) {
	s := mcpserver.NewMCPServer(
		"apihub-mcp",
		"0.0.1",
		mcpserver.WithToolCapabilities(false),
		mcpserver.WithInstructions(`The apihub-mcp MCP server provides information about REST API specifications.

DATA STRUCTURE:
- REST API specifications are organized into packages
- Package ID can serve as a hint to which domain the API belongs
- Each package contains API operations
- Each package can have multiple versions in YYYY.Q format (e.g., 2024.3, 2024.4)

WHEN TO USE THIS SERVER:
Use apihub-mcp when the user asks about:
- REST API operations and endpoints
- Available API specifications
- How to create or get resources via API
- Detailed information about specific API operations

AVAILABLE TOOLS:
1. search_rest_api_operations - search for API operations (see tool description for details)
2. get_rest_api_operations_specification - get OpenAPI specification for a specific operation (use only when user explicitly requests details)

AVAILABLE RESOURCES:
- api-packages-list - list of all packages in the system. Use this resource when:
  * User asks "what packages are available", "show all APIs", "list packages"
  * You need to find package ID by package name for use in tool calls
  * The resource returns a JSON array with fields: name, id, type (package/group)
  * Use package ID from this resource in the 'group' parameter of search_rest_api_operations tool

RESPONSES:
- Provide concise and structured answers
- Return all metadata that MCP returns in responses
- First show a list of operations to choose from, even if only one operation is found
- Use get_rest_api_operations_specification only when user explicitly requests details about a specific operation`),
	)

	tools.AddToolsToServer(s, operationService)
	tools.AddResourcesToServer(s, packageService)

	handler := mcpserver.NewStreamableHTTPServer(s)
	return handler, nil
}
