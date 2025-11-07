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

package mcp

import (
	"net/http"

	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/service"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
)

func InitMcpHandler(operationService service.OperationService) (http.Handler, error) {
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
									Provide compact strucutrued asnwers.`),
	)

	// addResources(s, operationService)
	AddToolsToServer(s, operationService)

	handler := mcpserver.NewStreamableHTTPServer(s)
	return handler, nil
}

/* func addResources(s *mcpserver.MCPServer, operationService service.OperationService) {

// probably makes sense to provide package group ids list

	s.AddResource(
		mcp.Resource{
			URI:         "res://rest_operation_specification/{operationId}",
			MIMEType:    "application/json",
			Name:        "REST API Operation Specification by operation ID",
			Description: "Open API specification which contains only one operation with everything around, like involved DTOs, response codes, etc",
		},
		func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			operationId := request.Params.URI
			// Extract ID from URI (should be res://rest_operation_specification/{operationId})
			operationId = strings.TrimPrefix(operationId, "res://rest_operation_specification/")

			searchReq := view.OperationBasicSearchReq{
				//PackageId:   "",
				//Version:     "",
				OperationId: operationId,
				// Revision:    0,
				ApiType: string(view.RestApiType),
			}

			operationViewInterface, err := operationService.GetOperation(searchReq)

			if err != nil {
				return nil, err
			}

			operationView, ok := operationViewInterface.(*view.RestOperationSingleView)
			if !ok {
				return nil, fmt.Errorf("unexpected operation view type")
			}

			return []mcp.ResourceContents{
				mcp.TextResourceContents{
					URI:      request.Params.URI,
					MIMEType: "application/json",
					Text:     toJSONString(operationView.Data),
				},
			}, nil
		},
	)
} */

type TransformedOperation struct {
	OperationId string `json:"operationId"`
	ApiKind     string `json:"apiKind"`
	ApiType     string `json:"apiType"`
	ApiAudience string `json:"apiAudience"`
	Path        string `json:"path"`
	Method      string `json:"method"`
	PackageId   string `json:"packageId"`
	PackageName string `json:"packageName"`
	Version     string `json:"version"`
	Title       string `json:"title"`
}

func transformOperations(items []view.RestOperationSearchResult) []TransformedOperation {
	transformed := make([]TransformedOperation, len(items))

	for i, item := range items {
		transformed[i] = TransformedOperation{
			OperationId: item.OperationId,
			ApiKind:     item.ApiKind,
			ApiType:     item.ApiType,
			ApiAudience: item.ApiAudience,
			Path:        item.Path,
			Method:      item.Method,
			PackageId:   item.PackageId,
			PackageName: item.PackageName,
			Version:     item.Version,
			Title:       item.Title,
		}
	}

	return transformed
}
