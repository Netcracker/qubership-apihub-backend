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

package service

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
	ToolDescriptionSearchOperationsMCP = `Search for REST API operations by text query.

IMPORTANT: The search is not full-text. For example, a query "create customer" may not find an operation "create new customer". Therefore, it is important to try different search query variations.

LLM INSTRUCTIONS:
- For the first call, use a large limit (100) to find as many options as possible
- Consider simplifying the query to a single keyword (e.g., if query is "create customer", also try "customer")
- Query string has special features: -word to force exclude a word from the search - it can help if search results are flooded with irrelevant results; "something certain"  - double quotes to strict search of a phrase/word
- Group results by packageId when displaying
- Return all metadata that MCP returns (operationId, packageId, packageName, version, path, method, title, apiKind, apiType, apiAudience)
- Return the most recent versions of operations (by default, search is performed in the latest completed version)
- If the first call returned few unique operations - make repeated calls:
  * Increase page number for pagination
  * Simplify or generalize the search query
  * Search in other packages (use 'group' parameter for a specific package)
  * Search in older versions only if user explicitly requested it
- If user asks for more results - increment page, simplify query, or search in other packages/versions
- DO NOT use get_rest_api_operations_specification in advance - first show a list of operations to choose from, even if only one is found
- Use get_rest_api_operations_specification only when user explicitly requests details about a specific operation
- If user explicitly requests a specific version - use 'release' parameter in YYYY.Q format
- If user requests results from a specific package - use 'group' parameter with packageId (not packageName)`

	ToolDescriptionGetOperationSpecMCP = `Get OpenAPI specification for a specific REST API operation.

Use this tool ONLY when the user explicitly requests details about a specific operation.

LLM INSTRUCTIONS:
- The response contains JSON with REST API specification - provide the full specification json in the response, display it as a code block
- After the code block, add a human-readable description:
  * Purpose and meaning of the operation
  * Description of request parameters
  * Description of response structure
  * Specify the package (packageId) and version in which this operation is located
- Generate RequestBody and ResponseBody examples based on the specification
- Provide the user with complete information about the operation
- Include the full OpenAPI specification json in the response`
)

// Tool descriptions for OpenAI
const (
	ToolDescriptionSearchOperationsOpenAI = `Search for REST API operations by text query.

IMPORTANT: The search is not full-text. For example, a query "create customer" may not find an operation "create new customer". Therefore, it is important to try different search query variations.

LLM INSTRUCTIONS:
- For the first call, use a large limit (100) to find as many options as possible
- Consider simplifying the query to a single keyword (e.g., if query is "create customer", also try "customer")
- Query string has special features: -word to force exclude a word from the search - it can help if search results are flooded with irrelevant results; "something certain"  - double quotes to strict search of a phrase/word
- Group results by packageId when displaying in markdown format
- Return all metadata that MCP returns (operationId, packageId, packageName, version, path, method, title, apiKind, apiType, apiAudience)
- Return the most recent versions of operations (by default, search is performed in the latest completed version)
- If the first call returned few unique operations - make repeated calls:
  * Increase page number for pagination
  * Simplify or generalize the search query
  * Search in other packages (use 'group' parameter for a specific package)
  * Search in older versions only if user explicitly requested it
- If user asks for more results - increment page, simplify query, or search in other packages/versions
- DO NOT use get_rest_api_operations_specification in advance - first show a list of operations to choose from in markdown format, even if only one is found
- Use get_rest_api_operations_specification only when user explicitly requests details about a specific operation
- If user explicitly requests a specific version - use 'release' parameter in YYYY.Q format
- If user requests results from a specific package - use 'group' parameter with packageId (not packageName)
- REQUIRED: Convert metadata to markdown links (relative, without baseUrl):
  * packageId -> [packageId](/portal/packages/<packageId>)
  * operationId -> [operationId](/portal/packages/<packageId>/<version>/operations/rest/<operationId>)
- Format responses in markdown with well-readable markup (headings, lists, tables)`

	ToolDescriptionGetOperationSpecOpenAI = `Get OpenAPI specification for a specific REST API operation.

Use this tool ONLY when the user explicitly requests details about a specific operation.

LLM INSTRUCTIONS:
- The response contains JSON with REST API specification - provide the full specification json in the response, display it as a code block
- After the code block, add a human-readable description in markdown format:
  * Purpose and meaning of the operation
  * Description of request parameters
  * Description of response structure
  * Specify the package (packageId) and version in which this operation is located
- Generate RequestBody and ResponseBody examples based on the specification in markdown code blocks
- Provide the user with complete information about the operation in structured markdown format
- Use markdown links for packageId and operationId:
  * packageId -> [packageId](/portal/packages/<packageId>)
  * operationId -> [operationId](/portal/packages/<packageId>/<version>/operations/rest/<operationId>)`
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
			"query":   "Text search query for finding API operations. Important: search is not full-text, so it's worth trying different query variations (simplified, with keywords)",
			"limit":   "Maximum number of results to return (10-100). For the first search, it's recommended to use 100",
			"page":    "Page number for pagination (starts from 0). Use to get additional results",
			"release": "Release version in YYYY.Q format (e.g., 2024.3). By default, the latest completed version is used. Specify only if user explicitly requests a specific version",
			"group":   "Package ID (packageId) to filter search by a specific package. Use packageId from api-packages-list resource, not packageName",
		},
		ToolNameGetOperationSpec: {
			"operationId": "Unique operation identifier (operationId) from search results",
			"packageId":   "Package ID (packageId) where the operation is located. Use packageId from search results or api-packages-list resource",
			"version":     "Package version in YYYY.Q format (e.g., 2024.3) where the operation is located",
		},
	}

	if toolDescs, ok := descriptions[toolName]; ok {
		if desc, ok := toolDescs[paramName]; ok {
			return desc
		}
	}
	return ""
}

// getToolMetadata returns metadata for all tools
func getToolMetadata() []view.ToolMetadata {
	return []view.ToolMetadata{
		{
			Name:              ToolNameSearchOperations,
			Schema:            searchOperationsSchema,
			DescriptionMCP:    ToolDescriptionSearchOperationsMCP,
			DescriptionOpenAI: ToolDescriptionSearchOperationsOpenAI,
		},
		{
			Name:              ToolNameGetOperationSpec,
			Schema:            getOperationSpecSchema,
			DescriptionMCP:    ToolDescriptionGetOperationSpecMCP,
			DescriptionOpenAI: ToolDescriptionGetOperationSpecOpenAI,
		},
	}
}

// AddToolsToServer registers MCP tools to the provided MCP server
func AddToolsToServer(s *mcpserver.MCPServer, operationService OperationService) {
	toolsMetadata := getToolMetadata()

	// Add search_rest_api_operations tool
	searchMeta := toolsMetadata[0]
	s.AddTool(mcp.Tool{
		Name:           searchMeta.Name,
		Description:    searchMeta.DescriptionMCP,
		RawInputSchema: searchMeta.Schema,
	}, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return ExecuteSearchTool(ctx, req, operationService)
	})

	// Add get_rest_api_operations_specification tool
	specMeta := toolsMetadata[1]
	s.AddTool(mcp.Tool{
		Name:           specMeta.Name,
		Description:    specMeta.DescriptionMCP,
		RawInputSchema: specMeta.Schema,
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
		if err := json.Unmarshal(meta.Schema, &schemaMap); err != nil {
			log.Errorf("Failed to unmarshal schema for tool %s: %v", meta.Name, err)
			continue
		}

		// Add descriptions to parameters for OpenAI format
		enhancedSchema := enhanceSchemaWithDescriptions(schemaMap, meta.Name)

		result[i] = map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        meta.Name,
				"description": meta.DescriptionOpenAI,
				"parameters":  enhancedSchema,
			},
		}
	}

	return result
}

// AddResourcesToServer registers MCP resources to the provided MCP server
func AddResourcesToServer(s *mcpserver.MCPServer, packageService PackageService) {
	mcpWorkspace := os.Getenv("MCP_WORKSPACE")
	if mcpWorkspace == "" {
		log.Warn("MCP_WORKSPACE environment variable is not set, skipping API packages resource registration")
		return
	}

	// Register API packages resource
	s.AddResource(mcp.Resource{
		URI:         "api-packages-list",
		Name:        "API Packages List",
		Description: "List of all API packages in the system. The resource returns a JSON array with elements containing: name (package/group name), id (package ID for use in tool calls), type (type: 'package' or 'group'). Package ID can serve as a hint to which domain the API belongs. Use this resource to: get a list of all available packages, find package ID by package name. Package IDs from this resource should be used in the 'group' parameter of the search_rest_api_operations tool.",
		MIMEType:    "application/json",
	}, func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		return GetPackagesList(ctx, packageService, mcpWorkspace)
	})
}

// GetPackagesList retrieves the list of packages from the workspace
func GetPackagesList(ctx context.Context, packageService PackageService, workspaceId string) ([]mcp.ResourceContents, error) {
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

	// Post-processing: filter and convert packages
	packagesMCP := convertPackagesToMCP(packages)

	jsonData, err := json.Marshal(packagesMCP)
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
func ExecuteSearchTool(ctx context.Context, req mcp.CallToolRequest, operationService OperationService) (*mcp.CallToolResult, error) {
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

	searchResult, err := operationService.LiteSearchForOperations(searchReq)
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
func ExecuteGetSpecTool(ctx context.Context, req mcp.CallToolRequest, operationService OperationService) (*mcp.CallToolResult, error) {
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

// convertPackagesToMCP filters and converts Packages to PackagesMCP
// Removes packages with packageId containing ".RUNENV." and excludes defaultRole, permissions, releaseVersionPattern, createdAt, IsFavorite, ImageUrl, DeletedAt fields
func convertPackagesToMCP(packages *view.Packages) *view.PackagesMCP {
	if packages == nil {
		return &view.PackagesMCP{Packages: []view.PackagesInfoMCP{}}
	}

	// Filter out packages with packageId containing ".RUNENV."
	filtered := make([]view.PackagesInfo, 0, len(packages.Packages))
	for _, pkg := range packages.Packages {
		if !strings.Contains(pkg.Id, ".RUNENV.") {
			filtered = append(filtered, pkg)
		}
	}

	// Convert to PackagesInfoMCP (excluding defaultRole, permissions, releaseVersionPattern, createdAt, IsFavorite, ImageUrl, DeletedAt)
	converted := make([]view.PackagesInfoMCP, len(filtered))
	for i, pkg := range filtered {
		converted[i] = view.PackagesInfoMCP{
			Id:                        pkg.Id,
			Alias:                     pkg.Alias,
			ParentId:                  pkg.ParentId,
			Kind:                      pkg.Kind,
			Name:                      pkg.Name,
			Description:               pkg.Description,
			ServiceName:               pkg.ServiceName,
			Parents:                   pkg.Parents,
			LastReleaseVersionDetails: pkg.LastReleaseVersionDetails,
			RestGroupingPrefix:        pkg.RestGroupingPrefix,
		}
	}

	return &view.PackagesMCP{Packages: converted}
}

// transformOperations transforms view.RestOperationSearchResult to TransformedOperation
func transformOperations(items []view.RestOperationSearchResult) []view.TransformedOperation {
	transformed := make([]view.TransformedOperation, len(items))

	for i, item := range items {
		transformed[i] = view.TransformedOperation{
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
