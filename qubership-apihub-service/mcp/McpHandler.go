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
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/service"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"

	log "github.com/sirupsen/logrus"
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
	addTools(s, operationService)

	handler := mcpserver.NewStreamableHTTPServer(s)
	return handler, nil
}

func addTools(s *mcpserver.MCPServer, operationService service.OperationService) {
	s.AddTool(mcp.Tool{
		Name: "search_rest_api_operations",
		Description: `Full-text search for REST API operations.
			LLM INSTRUCTIONS:
			- Group methods by packageIds;
			- If user ask for more result - increase page and ask this tool again;
			- If user ask for more results from particular packageId - set parameter 'group' to this packageId and ask this tool again;
			- If users ask to provide details for concrete operation - ask tool get_rest_api_operations_specification;
			- Do not ask tool get_rest_api_operations_specification in advance, only if user asks for details for concrete operation;
			- If user asks for release version - set parameter 'release' to this version and ask this tool again. Release version is in format YYYY.Q;`,
		RawInputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"query": {
					"type": "string"
				},
				"limit": {
					"type": "integer",
					"minimum": 10,
					"maximum": 100
				},
				"page": {
				    "type": "integer"
				},
				"release": {
					"type": "string"
				},
				"group": {
					"type": "string"
				}
			},
			"required": ["query"]
		}`),
	}, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		q, err := req.RequireString("query")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		mcpWorkspace := os.Getenv("MCP_WORKSPACE")

		limit := req.GetInt("limit", 100)
		page := req.GetInt("page", 0)
		group := req.GetString("group", mcpWorkspace)
		releaseVersion := req.GetString("release", calculateNearestCompletedReleaseVersion())

		log.Infof("search_rest_api_operations: query=%s, limit=%d, page=%d, group=%s, releaseVersion=%s", q, limit, page, group, releaseVersion)

		if !strings.HasPrefix(group, mcpWorkspace) {
			log.Errorf("Group parameter should start with %s. Given: %s", mcpWorkspace, group)
			return mcp.NewToolResultError(fmt.Sprintf("Requested package is not allowed for search, only packages from workspace %s are allowed", mcpWorkspace)), nil
		}

		var packageIds []string
		if group != "" {
			packageIds = []string{group}
		}

		searchReq := view.SearchQueryReq{
			SearchString: q,
			PackageIds:   packageIds,
			Versions:     []string{releaseVersion},
			Statuses:     []string{"release"},
			OperationSearchParams: &view.OperationSearchParams{
				ApiType: string(view.RestApiType),
			},
			Limit: limit,
			Page:  page,
		}

		searchResult, err := operationService.SearchForOperations(searchReq)

		if err != nil {
			return nil, err
		}

		operations := make([]view.RestOperationSearchResult, len(*searchResult.Operations))
		for i, op := range *searchResult.Operations {
			operations[i] = op.(view.RestOperationSearchResult)
		}
		payload := map[string]any{"items": transformOperations(operations)}

		return mcp.NewToolResultStructuredOnly(payload), nil
	})

	s.AddTool(mcp.Tool{
		Name: "get_rest_api_operations_specification",
		Description: `Get OpenAPI specification file for REST API operation.
		            LLM INSTRUCTIONS:
					- The reponse is json with REST API specification - render it as a code block, maybe not full specification, but enough to understand the operation and DTOs;
					- After code block add some human-redabale summary about REST operation and DTOs from this specification and provide it to the user;
					- Also generate RequestBody and ResponseBody examples based on the specification and provide them to the user;`,
		RawInputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"operationId": {
					"type": "string"
				},
				"packageId": {
					"type": "string"
				},
				"version": {
					"type": "string"
				}
			},
			"required": ["operationId","packageId","version"]
		}`),
	}, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		operationId, err := req.RequireString("operationId")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		packageId, err := req.RequireString("packageId")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		version, err := req.RequireString("version")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		log.Infof("get_rest_api_operations_specification: operationId=%s, packageId=%s, version=%s", operationId, packageId, version)

		searchReq := view.OperationBasicSearchReq{
			PackageId:   packageId,
			Version:     version,
			OperationId: operationId,
			// Revision:    0,  // todo
			ApiType: string(view.RestApiType),
		}

		operationViewInterface, err := operationService.GetOperation(searchReq)

		if err != nil {
			return nil, err
		}

		operationView := (*operationViewInterface.(*interface{})).(view.RestOperationSingleView) // todo: dirty hack

		payload := map[string]any{"operationData": operationView.Data}

		return mcp.NewToolResultStructuredOnly(payload), nil
	})
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

func calculateNearestCompletedReleaseVersion() string {
	t := time.Now()
	year := t.Year()
	month := int(t.Month())

	// Calculate current quarter (1..4)
	currentQuarter := (month-1)/3 + 1

	// Move to previous quarter
	prevQuarter := currentQuarter - 1
	if prevQuarter == 0 {
		prevQuarter = 4
		year -= 1
	}

	return fmt.Sprintf("%d.%d", year, prevQuarter)
}

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
