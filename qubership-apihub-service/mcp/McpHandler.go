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
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// ----------------------

type Spec struct {
	ID       string
	Name     string
	MimeType string
	Body     []byte
}

type Operation struct {
	ID        string
	SpecID    string
	Path      string
	Method    string
	Summary   string
	Operation string
}

var specs = map[string]Spec{
	"customers-v1": {
		ID:       "customers-v1",
		Name:     "Customers API v1",
		MimeType: "application/json",
		Body:     []byte(`{"openapi":"3.0.3","info":{"title":"Customers API","version":"1.0.0"},"paths":{"/customers":{"post":{"operationId":"CreateCustomer","summary":"Create a customer","responses":{"201":{"description":"created"}}},"get":{"operationId":"ListCustomers","summary":"List customers","responses":{"200":{"description":"ok"}}}}}}`),
	},
	"orders-v1": {
		ID:       "orders-v1",
		Name:     "Orders API v1",
		MimeType: "application/json",
		Body:     []byte(`{"openapi":"3.0.3","info":{"title":"Orders API","version":"1.0.0"},"paths":{"/orders":{"post":{"operationId":"CreateOrder","summary":"Create order","responses":{"201":{"description":"created"}}}}}}`),
	},
}

var operations = []Operation{
	{ID: "op-1", SpecID: "customers-v1", Path: "/customers", Method: "POST", Summary: "Create a customer", Operation: "CreateCustomer"},
	{ID: "op-2", SpecID: "customers-v1", Path: "/customers", Method: "GET", Summary: "List customers", Operation: "ListCustomers"},
	{ID: "op-3", SpecID: "orders-v1", Path: "/orders", Method: "POST", Summary: "Create order", Operation: "CreateOrder"},
}

func findSpec(id string) (Spec, bool) {
	sp, ok := specs[id]
	return sp, ok
}

func listOps(specID, path, method string) []Operation {
	out := make([]Operation, 0)
	for _, op := range operations {
		if specID != "" && op.SpecID != specID {
			continue
		}
		if path != "" && op.Path != path {
			continue
		}
		if method != "" && !strings.EqualFold(op.Method, method) {
			continue
		}
		out = append(out, op)
	}
	return out
}

func searchAPIs(query string, limit int) []map[string]any {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	res := make([]map[string]any, 0)
	for _, sp := range specs {
		if strings.Contains(strings.ToLower(sp.Name), q) || strings.Contains(strings.ToLower(string(sp.Body)), q) {
			res = append(res, map[string]any{"type": "spec", "id": sp.ID, "name": sp.Name})
		}
	}
	for _, op := range operations {
		hay := strings.ToLower(op.Path + " " + op.Method + " " + op.Summary + " " + op.Operation)
		if strings.Contains(hay, q) {
			res = append(res, map[string]any{"type": "operation", "id": op.ID, "specId": op.SpecID, "path": op.Path, "method": op.Method, "summary": op.Summary})
		}
	}
	if limit > 0 && len(res) > limit {
		res = res[:limit]
	}
	return res
}

// ------------------------------

func InitMcpHandler() (http.Handler, error) {
	s := mcpserver.NewMCPServer(
		"apihub-mcp",
		"0.0.1",
		mcpserver.WithToolCapabilities(false),
		// todo mcpserver.WithInstructions("todo"),
	)

	// ----- Resources: res://specs/{id}
	s.AddResource(
		mcp.Resource{
			URI:      "res://specs/{id}",
			MIMEType: "application/json",
			Name:     "API Spec by ID",
		},
		func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			id := request.Params.URI
			// Extract ID from URI (should be res://specs/{id})
			if strings.HasPrefix(id, "res://specs/") {
				id = strings.TrimPrefix(id, "res://specs/")
			}
			if sp, ok := findSpec(id); ok {
				return []mcp.ResourceContents{
					mcp.TextResourceContents{
						URI:      request.Params.URI,
						MIMEType: "application/json",
						Text:     string(sp.Body),
					},
				}, nil
			}
			return nil, fmt.Errorf("spec not found: %s", id)
		},
	)

	// ----- Tools
	// search_apis
	s.AddTool(mcp.Tool{
		Name:           "search_apis",
		Description:    "Full-text search across specs and operations",
		RawInputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"integer","minimum":1,"maximum":100}},"required":["query"]}`),
	}, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		q, err := req.RequireString("query")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		limit := req.GetInt("limit", 25)
		items := searchAPIs(q, limit)
		payload := map[string]any{"items": items}
		return mcp.NewToolResultStructuredOnly(payload), nil
	})

	// get_spec
	s.AddTool(mcp.Tool{
		Name:           "get_spec",
		Description:    "Return spec metadata and a resource URI to fetch its body",
		RawInputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`),
	}, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, err := req.RequireString("id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if sp, ok := findSpec(id); ok {
			resp := map[string]any{
				"id":       sp.ID,
				"name":     sp.Name,
				"mimeType": sp.MimeType,
				"resource": "res://specs/" + sp.ID,
			}
			return mcp.NewToolResultStructuredOnly(resp), nil
		}
		return mcp.NewToolResultError("spec not found"), nil
	})

	// list_operations
	s.AddTool(mcp.Tool{
		Name:           "list_operations",
		Description:    "List operations filtered by specId/path/method",
		RawInputSchema: json.RawMessage(`{"type":"object","properties":{"specId":{"type":"string"},"path":{"type":"string"},"method":{"type":"string"}}}`),
	}, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		specID := req.GetString("specId", "")
		path := req.GetString("path", "")
		method := req.GetString("method", "")
		ops := listOps(specID, path, method)
		return mcp.NewToolResultStructuredOnly(map[string]any{"items": ops}), nil
	})

	handler := mcpserver.NewStreamableHTTPServer(s)
	return handler, nil
}
