package view

type McpEntityListView struct {
	Entities []interface{} `json:"entities"`
}

type McpEntityView struct {
	EntityId    string      `json:"entityId"`
	Kind        string      `json:"kind"`
	Name        string      `json:"name,omitempty"`
	McpEndpoint string      `json:"mcpEndpoint"`
	DocumentId  string      `json:"documentId,omitempty"`
	PackageRef  string      `json:"packageRef,omitempty"`
	Metadata    interface{} `json:"metadata,omitempty"`
}

type McpEntityDetailView struct {
	McpEntityView
	Data interface{} `json:"data,omitempty"`
}

const McpKindInit     = "init"
const McpKindTool     = "tool"
const McpKindPrompt   = "prompt"
const McpKindResource = "resource"

type McpEntitySearchResult struct {
	PackageId      string   `json:"packageId"`
	PackageName    string   `json:"name"`
	ParentPackages []string `json:"parentPackages"`
	VersionStatus  string   `json:"status"`
	Version        string   `json:"version"`
	EntityId       string   `json:"entityId"`
	Kind           string   `json:"kind"`
	Name           string   `json:"entityName,omitempty"`
	McpEndpoint    string   `json:"mcpEndpoint"`
}

// URL segment → kind mapping
var McpEntitySegmentToKind = map[string]string{
	"init":      McpKindInit,
	"tools":     McpKindTool,
	"prompts":   McpKindPrompt,
	"resources": McpKindResource,
}
