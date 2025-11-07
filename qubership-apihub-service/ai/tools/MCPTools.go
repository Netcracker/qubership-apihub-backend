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

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	secctx "github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/context"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/entity"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/service"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"

	log "github.com/sirupsen/logrus"
)

// AddToolsToServer registers MCP tools to the provided MCP server
func AddToolsToServer(s *mcpserver.MCPServer, operationService service.OperationService) {
	// Add search_rest_api_operations tool
	s.AddTool(mcp.Tool{
		Name: "search_rest_api_operations",
		Description: `Full-text search for REST API operations.
			LLM INSTRUCTIONS:
			- Group methods by packageIds;
			- If user ask for more result - increase page and ask this tool again;
			- If user ask for more results from particular packageId - set parameter 'group' to this packageId and ask this tool again;
			- If users ask to provide details for concrete operation - ask tool get_rest_api_operations_specification. Use packageId as a parameter for this tool (not packageName);
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
		return ExecuteSearchTool(ctx, req, operationService)
	})

	// Add get_rest_api_operations_specification tool
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
		return ExecuteGetSpecTool(ctx, req, operationService)
	})
}

// ExecuteSearchTool executes the search_rest_api_operations tool
func ExecuteSearchTool(ctx context.Context, req mcp.CallToolRequest, operationService service.OperationService) (*mcp.CallToolResult, error) {
	q, err := req.RequireString("query")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	mcpWorkspace := os.Getenv("MCP_WORKSPACE")

	limit := req.GetInt("limit", 100)
	page := req.GetInt("page", 0)
	group := req.GetString("group", mcpWorkspace)
	releaseVersion := req.GetString("release", CalculateNearestCompletedReleaseVersion())

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

	// Log MCP tool response at debug level
	payloadJSON, _ := json.Marshal(payload)
	log.Debugf("MCP tool search_rest_api_operations response: %s", string(payloadJSON))

	return mcp.NewToolResultStructuredOnly(payload), nil
}

// ExecuteGetSpecTool executes the get_rest_api_operations_specification tool
func ExecuteGetSpecTool(ctx context.Context, req mcp.CallToolRequest, operationService service.OperationService) (*mcp.CallToolResult, error) {
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
		ApiType:     string(view.RestApiType),
	}

	operationViewInterface, err := operationService.GetOperation(searchReq)
	if err != nil {
		return nil, err
	}

	operationView := (*operationViewInterface.(*interface{})).(view.RestOperationSingleView)

	payload := map[string]any{"operationData": operationView.Data}

	// Log MCP tool response at debug level
	payloadJSON, _ := json.Marshal(payload)
	log.Debugf("MCP tool get_rest_api_operations_specification response: %s", string(payloadJSON))

	return mcp.NewToolResultStructuredOnly(payload), nil
}

// CalculateNearestCompletedReleaseVersion calculates the nearest completed release version
func CalculateNearestCompletedReleaseVersion() string {
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

// MCPToolRequestWrapper wraps arguments for creating mcp.CallToolRequest
type MCPToolRequestWrapper struct {
	Name      string
	Arguments []byte
}

// ToCallToolRequest converts the wrapper to mcp.CallToolRequest
func (r *MCPToolRequestWrapper) ToCallToolRequest() mcp.CallToolRequest {
	// Parse arguments as map[string]any for GetArguments() to work correctly
	var args map[string]any
	if err := json.Unmarshal(r.Arguments, &args); err != nil {
		// If unmarshal fails, use empty map
		args = make(map[string]any)
	}

	return mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      r.Name,
			Arguments: args,
		},
	}
}

func (r *MCPToolRequestWrapper) RequireString(key string) (string, error) {
	var args map[string]interface{}
	if err := json.Unmarshal(r.Arguments, &args); err != nil {
		return "", fmt.Errorf("failed to unmarshal arguments: %w", err)
	}
	value, ok := args[key]
	if !ok {
		return "", fmt.Errorf("required parameter %s is missing", key)
	}
	str, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("parameter %s is not a string", key)
	}
	return str, nil
}

func (r *MCPToolRequestWrapper) GetString(key string, defaultValue string) string {
	var args map[string]interface{}
	if err := json.Unmarshal(r.Arguments, &args); err != nil {
		return defaultValue
	}
	value, ok := args[key]
	if !ok {
		return defaultValue
	}
	str, ok := value.(string)
	if !ok {
		return defaultValue
	}
	return str
}

func (r *MCPToolRequestWrapper) GetInt(key string, defaultValue int) int {
	var args map[string]interface{}
	if err := json.Unmarshal(r.Arguments, &args); err != nil {
		return defaultValue
	}
	value, ok := args[key]
	if !ok {
		return defaultValue
	}
	// JSON numbers are unmarshaled as float64
	if num, ok := value.(float64); ok {
		return int(num)
	}
	return defaultValue
}

// GetToolsForOpenAI returns MCP tools in OpenAI format
// This function extracts tool definitions from the MCP server and converts them to OpenAI format
func GetToolsForOpenAI() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"type": "function",
			"function": map[string]interface{}{
				"name": "search_rest_api_operations",
				"description": `Full-text search for REST API operations.
								LLM INSTRUCTIONS:
								- Group methods by packageIds;
								- Try to make initial search with big limit - 100;
								- If user ask for more result - increase limit and page numbers and ask this tool again;
								- If user ask for more results from particular packageId - set parameter 'group' to this packageId and ask this tool again;
								- If users ask to provide details for concrete operation - ask tool get_rest_api_operations_specification. Use packageId as a parameter for this tool (not packageName);
								- Do not ask tool get_rest_api_operations_specification in advance, only if user asks for details for concrete operation;
								- If user asks for release version - set parameter 'release' to this version and ask this tool again. Release version is in format YYYY.Q;
								- Please enrich each operation information with relative (with no base URL!) link to corresponding package in markdown format: [<packageID>](/portal/packages/<packageId>);`,
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"query": map[string]interface{}{
							"type":        "string",
							"description": "Search query string",
						},
						"limit": map[string]interface{}{
							"type":        "integer",
							"minimum":     10,
							"maximum":     100,
							"description": "Maximum number of results to return",
						},
						"page": map[string]interface{}{
							"type":        "integer",
							"description": "Page number for pagination",
						},
						"release": map[string]interface{}{
							"type":        "string",
							"description": "Release version in format YYYY.Q",
						},
						"group": map[string]interface{}{
							"type":        "string",
							"description": "Package group ID to filter by",
						},
					},
					"required": []string{"query"},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]interface{}{
				"name": "get_rest_api_operations_specification",
				"description": `Get OpenAPI specification file for REST API operation.
								LLM INSTRUCTIONS:
								- The reponse is json with REST API specification - render it as a code block, maybe not full specification, but enough to understand the operation and DTOs;
								- After code block add some human-redabale summary about REST operation and DTOs from this specification and provide it to the user;
								- Also generate RequestBody and ResponseBody examples based on the specification and provide them to the user;`,
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"operationId": map[string]interface{}{
							"type":        "string",
							"description": "Operation ID",
						},
						"packageId": map[string]interface{}{
							"type":        "string",
							"description": "Package ID",
						},
						"version": map[string]interface{}{
							"type":        "string",
							"description": "Version",
						},
					},
					"required": []string{"operationId", "packageId", "version"},
				},
			},
		},
	}
}

// transformOperations transforms view.RestOperationSearchResult to TransformedOperation
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

// TransformedOperation represents a transformed operation for MCP response
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

// PackageHierarchyItem represents a package or package group in the hierarchy
type PackageHierarchyItem struct {
	Name string `json:"name"`
	Id   string `json:"id"`
	Type string `json:"type"` // "package" or "group"
}

// AddResourcesToServer registers MCP resources to the provided MCP server
func AddResourcesToServer(s *mcpserver.MCPServer, packageService service.PackageService) {
	mcpWorkspace := os.Getenv("MCP_WORKSPACE")
	if mcpWorkspace == "" {
		log.Warn("MCP_WORKSPACE environment variable is not set, skipping API packages resource registration")
		return
	}

	// Register API packages resource
	s.AddResource(mcp.Resource{
		URI:         "api-packages-list",
		Name:        "API Packages List",
		Description: "List of API packages and package groups in the workspace. Each item has: name (package/group name), id (package ID for use in tool calls), and type (either 'package' or 'group'). Use this resource to: get list of available packages, find package IDs by name. Package IDs from this resource should be used in the 'group' parameter of search_rest_api_operations tool.",
		MIMEType:    "application/json",
	}, func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		return GetPackagesList(ctx, packageService, mcpWorkspace)
	})
}

// GetPackagesList retrieves the list of packages from the workspace
func GetPackagesList(ctx context.Context, packageService service.PackageService, workspaceId string) ([]mcp.ResourceContents, error) {
	log.Infof("Getting packages list for workspace: %s", workspaceId)

	// Create system context for service calls
	secCtx := secctx.CreateSystemContext()

	// Prepare request parameters as if called from packageControllerImpl.GetPackagesList
	// kind=package&showAllDescendants=true&parentId=<MCP_WORKSPACE>
	// showAllDescendants=true to get the full tree including all descendants
	// parentId=workspaceId to start from workspace
	packageListReq := view.PackageListReq{
		Kind:               []string{entity.KIND_PACKAGE}, // As specified: kind=package
		ShowAllDescendants: true,
		ParentId:           workspaceId,
		Limit:              10000, // Large limit to get all packages
		Offset:             0,
	}

	// Get all packages from workspace
	packages, err := packageService.GetPackagesList(secCtx, packageListReq, false)
	if err != nil {
		log.Errorf("Failed to get packages list: %v", err)
		return nil, fmt.Errorf("failed to get packages list: %w", err)
	}

	jsonData, err := json.Marshal(packages)
	if err != nil {
		log.Errorf("Failed to marshal packages list: %v", err)
		return nil, fmt.Errorf("failed to marshal packages list: %w", err)
	}

	log.Debugf("Packages list retrieved: %s", jsonData)

	return []mcp.ResourceContents{
		&mcp.TextResourceContents{
			URI:      "api-packages-list",
			MIMEType: "application/json",
			Text:     string(jsonData),
		},
	}, nil
}
