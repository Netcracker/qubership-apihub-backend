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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/ai/client"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/ai/tools"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/service"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
	mcpgo "github.com/mark3labs/mcp-go/mcp"

	log "github.com/sirupsen/logrus"
)

// System message base content for OpenAI chat
const systemMessageBaseContent = `You are a specialized assistant for working with REST API documentation and specifications. Your role is to help users find and understand API operations, endpoints, and their specifications.

IMPORTANT RESTRICTIONS:
- You MUST ONLY help with questions related to REST API documentation, API operations, endpoints, specifications, and related technical topics
- If a user asks about topics unrelated to API documentation (general knowledge, history, current events, personal advice, etc.), you MUST politely decline and explain that you can only help with API-related questions
- Example response for off-topic questions: "I'm sorry, but I specialize in helping with REST API documentation and specifications. I can't help with questions outside of this topic. Can I help you with something about APIs?"

DATA STRUCTURE:
- REST API specifications are organized into packages
- Package ID can serve as a hint to which domain the API belongs
- Each package contains API operations
- Each package can have multiple versions in YYYY.Q format (e.g., 2024.3, 2024.4)

YOUR CAPABILITIES:
- Search for REST API operations using the search_rest_api_operations tool
- Get detailed OpenAPI specifications for specific operations using the get_rest_api_operations_specification tool
- Access the api-packages-list resource to get a list of all available API packages
- Explain API endpoints, request/response formats, and data structures
- Help users understand how to use specific APIs

AVAILABLE RESOURCES:
- api-packages-list: A resource containing the list of all API packages in the system. This resource is useful when:
  * User asks "what packages are available", "show all APIs", "list packages"
  * You need to find package ID by package name (use the ID in tool calls)
  * The resource returns a JSON array with elements containing: name, id, and type (package/group)
  * When searching for operations, use the package ID from this resource in the 'group' parameter of the search_rest_api_operations tool

RESPONSE FORMAT:
- Always use markdown format with well-readable markup (headings, lists, tables, code blocks)
- Respond concisely and in a structured manner
- Return all metadata that tools return
- Convert metadata to markdown links (relative, without baseUrl):
  * packageId -> [packageId](/portal/packages/<packageId>)
  * operationId -> [operationId](/portal/packages/<packageId>/<version>/operations/rest/<operationId>)
- First show a list of operations to choose from, even if only one operation is found
- Use get_rest_api_operations_specification only when user explicitly requests details about a specific operation

Always use available tools and resources when appropriate to provide accurate and up-to-date information about APIs.`

type ChatService interface {
	Chat(ctx context.Context, req view.ChatRequest) (*view.ChatResponse, error)
	ChatStream(ctx context.Context, req view.ChatRequest, writer io.Writer) error
}

func NewChatService(
	systemInfoService service.SystemInfoService,
	operationService service.OperationService,
	packageService service.PackageService,
) ChatService {
	// Create OpenAI client with proxy support and extended timeout
	proxyURL := systemInfoService.GetOpenAIProxyURL()
	httpClient := client.NewOpenAIClient(proxyURL)

	service := &chatServiceImpl{
		systemInfoService: systemInfoService,
		operationService:  operationService,
		packageService:    packageService,
		httpClient:        httpClient,
	}

	// Initialize MCP tools (no need to create MCP server - tools are defined statically)
	service.initMCPTools()
	log.Infof("ChatService initialized with %d MCP tools", len(service.mcpTools))
	for _, tool := range service.mcpTools {
		log.Debugf("MCP tool available: %s - %s", tool.Function.Name, tool.Function.Description)
	}

	return service
}

type chatServiceImpl struct {
	systemInfoService service.SystemInfoService
	operationService  service.OperationService
	packageService    service.PackageService
	httpClient        *http.Client
	mcpTools          []openAITool
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAITool struct {
	Type     string         `json:"type"`
	Function openAIFunction `json:"function"`
}

type openAIFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type openAIRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Tools       []openAITool    `json:"tools,omitempty"`
	ToolChoice  interface{}     `json:"tool_choice,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
	Temperature float64         `json:"temperature,omitempty"`
}

type openAIResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls,omitempty"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type openAIStreamChunk struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls,omitempty"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

func (c *chatServiceImpl) Chat(ctx context.Context, req view.ChatRequest) (*view.ChatResponse, error) {
	// Log incoming request - find last user message
	userMessage := ""
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			userMessage = req.Messages[i].Content
			break
		}
	}
	log.Debugf("Chat request received. Last user message: %s", userMessage)

	// Get MCP tools
	mcpTools := c.mcpTools
	log.Debugf("Using %d MCP tools for this request", len(mcpTools))

	// Convert messages
	messages := make([]openAIMessage, len(req.Messages))
	for i, msg := range req.Messages {
		messages[i] = openAIMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}
	}

	// Build OpenAI request
	openAIReq := openAIRequest{
		Model:       c.systemInfoService.GetOpenAIModel(),
		Messages:    messages,
		Tools:       mcpTools,
		Temperature: 0.7,
	}

	// Add system message if not present
	hasSystemMessage := false
	for _, msg := range messages {
		if msg.Role == "system" {
			hasSystemMessage = true
			break
		}
	}
	if !hasSystemMessage {
		systemContent := c.buildSystemMessage(ctx)
		systemMsg := openAIMessage{
			Role:    "system",
			Content: systemContent,
		}
		openAIReq.Messages = append([]openAIMessage{systemMsg}, openAIReq.Messages...)
	}

	// Make request to OpenAI
	reqBody, err := json.Marshal(openAIReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/chat/completions", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.systemInfoService.GetOpenAIApiKey())

	// Process tool calls in a loop until we get a final response
	currentMessages := openAIReq.Messages
	var finalResponse *openAIResponse
	maxIterations := 10 // Limit to prevent infinite loops
	totalUsage := openAIResponse{Usage: struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	}{}}

	for iteration := 0; iteration < maxIterations; iteration++ {
		// Trim message history if it gets too long to avoid context window overflow
		// Keep system message, recent conversation, and all tool-related messages from current session
		currentMessages = c.trimMessageHistory(currentMessages, iteration)

		// Make request to OpenAI
		reqBody, err := json.Marshal(openAIRequest{
			Model:       c.systemInfoService.GetOpenAIModel(),
			Messages:    currentMessages,
			Tools:       mcpTools,
			Temperature: 0.7,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}

		httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/chat/completions", bytes.NewBuffer(reqBody))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+c.systemInfoService.GetOpenAIApiKey())

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			return nil, fmt.Errorf("failed to make request to OpenAI: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("OpenAI API error: %d - %s", resp.StatusCode, string(body))
		}

		var openAIResp openAIResponse
		if err := json.NewDecoder(resp.Body).Decode(&openAIResp); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		if len(openAIResp.Choices) == 0 {
			return nil, fmt.Errorf("no choices in OpenAI response")
		}

		// Accumulate token usage
		totalUsage.Usage.PromptTokens += openAIResp.Usage.PromptTokens
		totalUsage.Usage.CompletionTokens += openAIResp.Usage.CompletionTokens
		totalUsage.Usage.TotalTokens += openAIResp.Usage.TotalTokens

		choice := openAIResp.Choices[0]
		finishReason := choice.FinishReason

		// If no tool calls, we have the final response
		if len(choice.Message.ToolCalls) == 0 || finishReason != "tool_calls" {
			finalResponse = &openAIResp
			log.Debugf("Got final response after %d iterations. Finish reason: %s", iteration+1, finishReason)
			break
		}

		// Handle tool calls
		log.Debugf("Iteration %d: OpenAI requested %d tool calls", iteration+1, len(choice.Message.ToolCalls))
		for i, toolCall := range choice.Message.ToolCalls {
			log.Debugf("Tool call %d: %s with arguments: %s", i+1, toolCall.Function.Name, toolCall.Function.Arguments)
		}

		// Execute tool calls and get results
		toolResults, err := c.executeToolCalls(ctx, choice.Message.ToolCalls)
		if err != nil {
			log.Errorf("Failed to execute tool calls: %v", err)
			// Continue with empty tool results
			toolResults = make([]string, len(choice.Message.ToolCalls))
		}
		if len(toolResults) > 0 {
			log.Debugf("Tool calls executed successfully, got %d results", len(toolResults))

			// Add assistant message with tool calls (required by OpenAI)
			toolCalls := make([]openAIToolCall, len(choice.Message.ToolCalls))
			for i, tc := range choice.Message.ToolCalls {
				toolCalls[i] = openAIToolCall{
					ID:   tc.ID,
					Type: tc.Type,
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				}
			}

			assistantMsg := openAIMessage{
				Role:      "assistant",
				Content:   "",
				ToolCalls: toolCalls,
			}

			// Add tool result messages (each must have tool_call_id)
			toolMessages := make([]openAIMessage, 0, len(choice.Message.ToolCalls))
			for i, toolCall := range choice.Message.ToolCalls {
				toolMessages = append(toolMessages, openAIMessage{
					Role:       "tool",
					Content:    toolResults[i],
					ToolCallID: toolCall.ID,
				})
			}

			// Build next iteration messages: current messages + assistant with tool_calls + tool results
			currentMessages = append(currentMessages, assistantMsg)
			currentMessages = append(currentMessages, toolMessages...)
		} else {
			// If no tool results, break to avoid infinite loop
			log.Warnf("No tool results, breaking tool call loop")
			finalResponse = &openAIResp
			break
		}
	}

	if finalResponse == nil {
		return nil, fmt.Errorf("reached maximum iterations (%d) without final response", maxIterations)
	}

	choice := finalResponse.Choices[0]
	responseMessage := view.ChatMessage{
		Role:    "assistant",
		Content: choice.Message.Content,
	}

	log.Debugf("Chat response generated after processing. Content length: %d, Total tokens used: %d", len(responseMessage.Content), totalUsage.Usage.TotalTokens)

	return &view.ChatResponse{
		Message: responseMessage,
		Usage: &view.ChatUsage{
			PromptTokens:     totalUsage.Usage.PromptTokens,
			CompletionTokens: totalUsage.Usage.CompletionTokens,
			TotalTokens:      totalUsage.Usage.TotalTokens,
		},
	}, nil
}

func (c *chatServiceImpl) ChatStream(ctx context.Context, req view.ChatRequest, writer io.Writer) error {
	// Log incoming request - find last user message
	userMessage := ""
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			userMessage = req.Messages[i].Content
			break
		}
	}
	log.Debugf("Chat stream request received. Last user message: %s", userMessage)

	// Get MCP tools
	mcpTools := c.mcpTools
	log.Debugf("Using %d MCP tools for this stream request", len(mcpTools))

	// Convert messages
	messages := make([]openAIMessage, len(req.Messages))
	for i, msg := range req.Messages {
		messages[i] = openAIMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}
	}

	// Build OpenAI request
	openAIReq := openAIRequest{
		Model:       c.systemInfoService.GetOpenAIModel(),
		Messages:    messages,
		Tools:       mcpTools,
		Stream:      true,
		Temperature: 0.7,
	}

	// Add system message if not present
	hasSystemMessage := false
	for _, msg := range messages {
		if msg.Role == "system" {
			hasSystemMessage = true
			break
		}
	}
	if !hasSystemMessage {
		systemContent := c.buildSystemMessage(ctx)
		systemMsg := openAIMessage{
			Role:    "system",
			Content: systemContent,
		}
		openAIReq.Messages = append([]openAIMessage{systemMsg}, openAIReq.Messages...)
	}

	// Make request to OpenAI
	reqBody, err := json.Marshal(openAIReq)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/chat/completions", bytes.NewBuffer(reqBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.systemInfoService.GetOpenAIApiKey())

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to make request to OpenAI: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("OpenAI API error: %d - %s", resp.StatusCode, string(body))
	}

	// Stream response
	decoder := json.NewDecoder(resp.Body)
	for {
		var chunk openAIStreamChunk
		if err := decoder.Decode(&chunk); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("failed to decode stream chunk: %w", err)
		}

		if len(chunk.Choices) > 0 {
			delta := chunk.Choices[0].Delta.Content
			if delta != "" {
				streamChunk := view.ChatStreamChunk{
					Delta: delta,
					Done:  chunk.Choices[0].FinishReason != "",
				}
				chunkJSON, _ := json.Marshal(streamChunk)
				writer.Write(chunkJSON)
				writer.Write([]byte("\n"))
			}
		}
	}

	return nil
}

func (c *chatServiceImpl) initMCPTools() {
	openAIToolsRaw := tools.GetToolsForOpenAI()
	toolsList := make([]openAITool, len(openAIToolsRaw))
	for i, toolRaw := range openAIToolsRaw {
		functionRaw := toolRaw["function"].(map[string]interface{})
		toolsList[i] = openAITool{
			Type: toolRaw["type"].(string),
			Function: openAIFunction{
				Name:        functionRaw["name"].(string),
				Description: functionRaw["description"].(string),
				Parameters:  functionRaw["parameters"].(map[string]interface{}),
			},
		}
	}

	c.mcpTools = toolsList
}

// buildSystemMessage builds system message with MCP resource data included
func (c *chatServiceImpl) buildSystemMessage(ctx context.Context) string {
	// Read MCP resource api-packages-list and include it in system message
	mcpWorkspace := os.Getenv("MCP_WORKSPACE")
	if mcpWorkspace != "" {
		resourceContents, err := tools.GetPackagesList(ctx, c.packageService, mcpWorkspace)
		if err != nil {
			log.Warnf("Failed to read api-packages-list resource: %v", err)
			return systemMessageBaseContent
		}

		if len(resourceContents) > 0 {
			if textContent, ok := resourceContents[0].(*mcpgo.TextResourceContents); ok {
				// Include resource data in system message
				resourceData := textContent.Text
				return systemMessageBaseContent + "\n\nCURRENT WORKSPACE PACKAGES (from api-packages-list resource):\n" + resourceData
			}
		}
	}

	return systemMessageBaseContent
}

func (c *chatServiceImpl) executeToolCalls(ctx context.Context, toolCalls []struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}) ([]string, error) {
	results := make([]string, len(toolCalls))

	for i, toolCall := range toolCalls {
		log.Debugf("Executing tool call: %s with args: %s", toolCall.Function.Name, toolCall.Function.Arguments)

		// Use MCP handlers to execute tool
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
			log.Errorf("Failed to parse tool arguments: %v", err)
			results[i] = fmt.Sprintf("Error parsing arguments: %v", err)
			continue
		}

		// Create MCP CallToolRequest using wrapper
		argsBytes, _ := json.Marshal(args)
		mcpReqWrapper := tools.MCPToolRequestWrapper{
			Name:      toolCall.Function.Name,
			Arguments: argsBytes,
		}

		// Convert wrapper to mcp.CallToolRequest
		mcpReq := mcpReqWrapper.ToCallToolRequest()

		// Execute via shared MCP tool handlers
		var result *mcpgo.CallToolResult
		var err error
		switch toolCall.Function.Name {
		case "search_rest_api_operations":
			result, err = tools.ExecuteSearchTool(ctx, mcpReq, c.operationService)
		case "get_rest_api_operations_specification":
			result, err = tools.ExecuteGetSpecTool(ctx, mcpReq, c.operationService)
		default:
			results[i] = fmt.Sprintf("Unknown tool: %s", toolCall.Function.Name)
			continue
		}

		if err != nil {
			log.Errorf("MCP tool execution failed: %v", err)
			results[i] = fmt.Sprintf("Error: %v", err)
			continue
		}

		// Log MCP tool response at debug level
		resultJSON, _ := json.Marshal(result.Content)
		log.Debugf("MCP tool %s returned result (IsError=%v, Content length=%d): %s",
			toolCall.Function.Name, result.IsError, len(result.Content), string(resultJSON))

		// Convert result to string
		if result.IsError {
			if len(result.Content) > 0 {
				// Content is an array of mcpgo.Content, which can be Text or Resource
				contentStr := ""
				for _, content := range result.Content {
					if textContent, ok := content.(mcpgo.TextContent); ok {
						contentStr += textContent.Text
					}
				}
				if contentStr == "" {
					contentStr = "Unknown error from tool"
				}
				log.Warnf("MCP tool returned error: %s", contentStr)
				results[i] = contentStr
			} else {
				results[i] = "Unknown error from tool"
			}
		} else {
			// Convert structured result to JSON string
			resultJSON, err := json.Marshal(result.Content)
			if err != nil {
				log.Errorf("Failed to marshal tool result: %v", err)
				results[i] = fmt.Sprintf("Error marshaling result: %v", err)
			} else {
				results[i] = string(resultJSON)
				log.Debugf("Tool %s executed successfully, result length: %d", toolCall.Function.Name, len(results[i]))
			}
		}

	}

	return results, nil
}

// trimMessageHistory trims the message history to prevent context window overflow
// Strategy:
// 1. Always keep system message (first message if it's system)
// 2. Keep recent conversation messages (last N user/assistant pairs)
// 3. Keep all tool-related messages (assistant with tool_calls and tool responses) from current session
// 4. Maximum messages limit: 50 (configurable)
func (c *chatServiceImpl) trimMessageHistory(messages []openAIMessage, currentIteration int) []openAIMessage {
	const maxMessages = 50             // Maximum number of messages to keep
	const recentConversationPairs = 10 // Number of recent user/assistant pairs to keep

	if len(messages) <= maxMessages {
		return messages
	}

	log.Debugf("Trimming message history: %d messages -> max %d", len(messages), maxMessages)

	// Find system message (usually first)
	var systemMsg *openAIMessage
	var systemMsgIndex = -1
	for i, msg := range messages {
		if msg.Role == "system" {
			systemMsg = &messages[i]
			systemMsgIndex = i
			break
		}
	}

	// Separate messages into categories
	var toolMessages []openAIMessage         // assistant with tool_calls and tool responses
	var conversationMessages []openAIMessage // user and assistant without tool_calls

	// Start from after system message
	startIdx := 0
	if systemMsgIndex >= 0 {
		startIdx = systemMsgIndex + 1
	}

	for i := startIdx; i < len(messages); i++ {
		msg := messages[i]

		// Tool-related messages: assistant with tool_calls or tool role
		if msg.Role == "tool" || (msg.Role == "assistant" && len(msg.ToolCalls) > 0) {
			toolMessages = append(toolMessages, msg)
		} else {
			// Regular conversation messages
			conversationMessages = append(conversationMessages, msg)
		}
	}

	// Keep only recent conversation pairs
	var trimmedConversation []openAIMessage
	if len(conversationMessages) > recentConversationPairs*2 {
		// Keep last N pairs (each pair = user + assistant)
		trimmedConversation = conversationMessages[len(conversationMessages)-recentConversationPairs*2:]
		log.Debugf("Trimmed conversation: kept last %d pairs (%d messages)", recentConversationPairs, len(trimmedConversation))
	} else {
		trimmedConversation = conversationMessages
	}

	// Reconstruct message history: system + recent conversation + all tool messages
	result := make([]openAIMessage, 0, len(trimmedConversation)+len(toolMessages)+1)

	// Add system message first if exists
	if systemMsg != nil {
		result = append(result, *systemMsg)
	}

	// Add recent conversation
	result = append(result, trimmedConversation...)

	// Add all tool messages (they are important for current session context)
	result = append(result, toolMessages...)

	log.Debugf("Message history trimmed: %d -> %d messages (system: %v, conversation: %d, tool: %d)",
		len(messages), len(result), systemMsg != nil, len(trimmedConversation), len(toolMessages))

	return result
}
