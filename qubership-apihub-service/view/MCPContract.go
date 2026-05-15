package view

type McpEntityListView struct {
	Entities []interface{} `json:"entities"`
}

type McpEntityView struct {
	EntityId    string      `json:"entityId"`
	Name        string      `json:"name,omitempty"`
	Title       string      `json:"title,omitempty"`
	Kind        string      `json:"kind"`
	ServerName  string      `json:"serverName"`
	McpEndpoint string      `json:"mcpEndpoint"`
	Deprecated  bool        `json:"deprecated"`
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

// URL segment → kind mapping
var McpEntitySegmentToKind = map[string]string{
	"init":      McpKindInit,
	"tools":     McpKindTool,
	"prompts":   McpKindPrompt,
	"resources": McpKindResource,
}
