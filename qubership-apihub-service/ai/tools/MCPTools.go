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

// Tool names constants
const (
	ToolNameSearchOperations = "search_rest_api_operations"
	ToolNameGetOperationSpec = "get_rest_api_operations_specification"
)

// Tool descriptions for MCP server
const (
	ToolDescriptionSearchOperationsMCP = `Full-text search for REST API operations.
		LLM INSTRUCTIONS:
		- Group methods by packageIds;
		- If user ask for more result - increase page and ask this tool again;
		- If user ask for more results from particular packageId - set parameter 'group' to this packageId and ask this tool again;
		- If users ask to provide details for concrete operation - ask tool get_rest_api_operations_specification. Use packageId as a parameter for this tool (not packageName);
		- Do not ask tool get_rest_api_operations_specification in advance, only if user asks for details for concrete operation;
		- If user asks for release version - set parameter 'release' to this version and ask this tool again. Release version is in format YYYY.Q;`

	ToolDescriptionGetOperationSpecMCP = `Get OpenAPI specification file for REST API operation.
	            LLM INSTRUCTIONS:
				- The reponse is json with REST API specification - render it as a code block, maybe not full specification, but enough to understand the operation and DTOs;
				- After code block add some human-redabale summary about REST operation and DTOs from this specification and provide it to the user;
				- Also generate RequestBody and ResponseBody examples based on the specification and provide them to the user;`
)

// Tool descriptions for OpenAI
const (
	ToolDescriptionSearchOperationsOpenAI = `Full-text search for REST API operations.
								LLM INSTRUCTIONS:
								- Group methods by packageIds;
								- Try to make initial search with big limit - 100;
								- If user ask for more result - increase limit and page numbers and ask this tool again;
								- If user ask for more results from particular packageId - set parameter 'group' to this packageId and ask this tool again;
								- If users ask to provide details for concrete operation - ask tool get_rest_api_operations_specification. Use packageId as a parameter for this tool (not packageName);
								- Do not ask tool get_rest_api_operations_specification in advance, only if user asks for details for concrete operation;
								- If user asks for release version - set parameter 'release' to this version and ask this tool again. Release version is in format YYYY.Q;
								- Please enrich each operation information with relative (with no base URL!) link to corresponding package in markdown format: [<packageID>](/portal/packages/<packageId>);`

	ToolDescriptionGetOperationSpecOpenAI = `Get OpenAPI specification file for REST API operation.
								LLM INSTRUCTIONS:
								- The reponse is json with REST API specification - render it as a code block, maybe not full specification, but enough to understand the operation and DTOs;
								- After code block add some human-redabale summary about REST operation and DTOs from this specification and provide it to the user;
								- Also generate RequestBody and ResponseBody examples based on the specification and provide them to the user;`
)

// Tool input schemas (shared between MCP and OpenAI)
var (
	searchOperationsSchema = json.RawMessage(`{
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
	}`)

	getOperationSpecSchema = json.RawMessage(`{
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
	}`)
)

// getParameterDescription returns description for a parameter
func getParameterDescription(toolName, paramName string) string {
	descriptions := map[string]map[string]string{
		ToolNameSearchOperations: {
			"query":   "Search query string",
			"limit":   "Maximum number of results to return",
			"page":    "Page number for pagination",
			"release": "Release version in format YYYY.Q",
			"group":   "Package group ID to filter by",
		},
		ToolNameGetOperationSpec: {
			"operationId": "Operation ID",
			"packageId":   "Package ID",
			"version":     "Version",
		},
	}

	if toolDescs, ok := descriptions[toolName]; ok {
		if desc, ok := toolDescs[paramName]; ok {
			return desc
		}
	}
	return ""
}

// Tool metadata structure
type toolMetadata struct {
	name              string
	schema            json.RawMessage
	descriptionMCP    string
	descriptionOpenAI string
}

// getToolMetadata returns metadata for all tools
func getToolMetadata() []toolMetadata {
	return []toolMetadata{
		{
			name:              ToolNameSearchOperations,
			schema:            searchOperationsSchema,
			descriptionMCP:    ToolDescriptionSearchOperationsMCP,
			descriptionOpenAI: ToolDescriptionSearchOperationsOpenAI,
		},
		{
			name:              ToolNameGetOperationSpec,
			schema:            getOperationSpecSchema,
			descriptionMCP:    ToolDescriptionGetOperationSpecMCP,
			descriptionOpenAI: ToolDescriptionGetOperationSpecOpenAI,
		},
	}
}

// AddToolsToServer registers MCP tools to the provided MCP server
func AddToolsToServer(s *mcpserver.MCPServer, operationService service.OperationService) {
	toolsMetadata := getToolMetadata()

	// Add search_rest_api_operations tool
	searchMeta := toolsMetadata[0]
	s.AddTool(mcp.Tool{
		Name:           searchMeta.name,
		Description:    searchMeta.descriptionMCP,
		RawInputSchema: searchMeta.schema,
	}, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return ExecuteSearchTool(ctx, req, operationService)
	})

	// Add get_rest_api_operations_specification tool
	specMeta := toolsMetadata[1]
	s.AddTool(mcp.Tool{
		Name:           specMeta.name,
		Description:    specMeta.descriptionMCP,
		RawInputSchema: specMeta.schema,
	}, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return ExecuteGetSpecTool(ctx, req, operationService)
	})
}

// GetToolsForOpenAI returns MCP tools in OpenAI format
// This function extracts tool definitions from the MCP server and converts them to OpenAI format
func GetToolsForOpenAI() []map[string]interface{} {
	toolsMetadata := getToolMetadata()
	result := make([]map[string]interface{}, len(toolsMetadata))

	for i, meta := range toolsMetadata {
		// Parse schema from JSON to map for OpenAI format
		var schemaMap map[string]interface{}
		if err := json.Unmarshal(meta.schema, &schemaMap); err != nil {
			log.Errorf("Failed to unmarshal schema for tool %s: %v", meta.name, err)
			continue
		}

		// Add descriptions to parameters for OpenAI format
		enhancedSchema := enhanceSchemaWithDescriptions(schemaMap, meta.name)

		result[i] = map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        meta.name,
				"description": meta.descriptionOpenAI,
				"parameters":  enhancedSchema,
			},
		}
	}

	return result
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

// enhanceSchemaWithDescriptions adds descriptions to schema properties for OpenAI format
func enhanceSchemaWithDescriptions(schema map[string]interface{}, toolName string) map[string]interface{} {
	// Create a copy to avoid modifying original
	enhanced := make(map[string]interface{})
	for k, v := range schema {
		enhanced[k] = v
	}

	// Add descriptions to properties
	if properties, ok := enhanced["properties"].(map[string]interface{}); ok {
		enhancedProperties := make(map[string]interface{})
		for propName, propValue := range properties {
			propMap, ok := propValue.(map[string]interface{})
			if !ok {
				enhancedProperties[propName] = propValue
				continue
			}

			// Add description if not present
			if _, hasDesc := propMap["description"]; !hasDesc {
				propMapCopy := make(map[string]interface{})
				for k, v := range propMap {
					propMapCopy[k] = v
				}
				propMapCopy["description"] = getParameterDescription(toolName, propName)
				enhancedProperties[propName] = propMapCopy
			} else {
				enhancedProperties[propName] = propValue
			}
		}
		enhanced["properties"] = enhancedProperties
	}

	return enhanced
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
