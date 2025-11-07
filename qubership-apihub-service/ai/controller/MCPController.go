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
		mcpserver.WithInstructions(`Use apihub-mcp if users asks for REST API operations - which operation can help to do something.
		                            You are an assistant for REST API documentation access. If users asks for avaialbe APIs, specifications, operations, ways how to create or get resources - 
									at first call one of the following tools:
									- search_rest_api_operations - full text search for REST API operations (see description for this tool for more details);
									- get_rest_api_operations_specification - when users asks for OpenAPI spec for particular API operation (see description for this tool for more details);
									If user's query is generic - start with search_rest_api_operations.
									
									AVAILABLE RESOURCES:
									- api-packages-list - A resource containing the list of API packages and package groups in the workspace. Use this resource to:
									  * Get a list of all available API packages when user asks "what packages are available", "list all APIs", "show me packages"
									  * Find package IDs when user mentions package names but you need the ID for tool calls
									  * The resource returns a JSON array with items containing: name, id, and type (package/group)
									  * When searching for operations, use the package ID from this resource in the 'group' parameter of search_rest_api_operations tool
									
									Provide compact strucutrued asnwers.`),
	)

	tools.AddToolsToServer(s, operationService)
	tools.AddResourcesToServer(s, packageService)

	handler := mcpserver.NewStreamableHTTPServer(s)
	return handler, nil
}
