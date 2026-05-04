package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
	mcpgo "github.com/mark3labs/mcp-go/mcp"

	log "github.com/sirupsen/logrus"
)

// ChatMessage is the LLM-side representation of a single message in a conversation.
// Used internally to feed history into the LLM client; not part of any HTTP DTO.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatUsage carries token usage stats returned by the LLM provider,
// used for compaction triggers and Prometheus metrics.
type ChatUsage struct {
	PromptTokens     int `json:"promptTokens"`
	CompletionTokens int `json:"completionTokens"`
	TotalTokens      int `json:"totalTokens"`
}

// toolNameAskClarification is a meta-tool that short-circuits the tool loop:
// instead of executing externally, the question becomes the assistant's response.
const toolNameAskClarification = "ask_clarification"

// System message base content — provider-agnostic, defines the assistant's scope and behaviour.
const systemMessageBaseContent = `You are a specialized assistant for working with REST API documentation and specifications. Your role is to help users find and understand API operations, endpoints, and their specifications, and to help them author Integration Design Specification (IDS) documents that describe how APIs are wired together.

IMPORTANT RESTRICTIONS:
- You MUST ONLY help with questions related to REST API documentation, API operations, endpoints, specifications, integration design and related technical topics
- If a user asks about topics unrelated to those areas (general knowledge, history, current events, personal advice, etc.), you MUST politely decline and explain that you can only help with API/integration-related questions
- Example response for off-topic questions: "I'm sorry, but I specialize in helping with REST API documentation, specifications and integration design. I can't help with questions outside of this topic. Can I help you with something about APIs?"

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
- Generate Integration Design Specification (IDS) documents on demand and deliver them to the user as downloadable Markdown files
- Ask a clarifying question using the ask_clarification tool when the request is genuinely ambiguous

INTEGRATION DESIGN GENERATION:
- When the user explicitly asks you to "generate", "create", "draft" or "build" an IDS / Integration Design Specification / design document for an integration scenario, your VERY FIRST action MUST be to call the start_ids_generation tool with the user's request as the user_input argument. The tool returns the canonical template, the step-by-step authoring rules, and a final hand-off contract.
- Follow the rules returned by start_ids_generation literally. They include MANDATORY APIHub lookups via search_rest_api_operations and get_rest_api_operations_specification for every API the user mentions; do NOT invent paths, parameters or schemas.
- When the document is complete, call save_generated_file with a concise filename (e.g. "IDS_<3rdPartySystemAbbrev>.md") and the FULL Markdown body. The tool returns a Markdown link of the form [filename](url); embed it verbatim in your final user-facing reply so the user can download the file. Keep the rest of the reply short -- one paragraph summarising what was generated.
- Never call save_generated_file outside of the IDS authoring flow, and never inline the IDS body itself in chat -- the user gets it via the download link.

CLARIFICATION POLICY:
- When a user's request is genuinely ambiguous and you cannot give a reliable answer without more details, call ask_clarification with ONE specific question instead of guessing or fabricating an answer.
- Use ask_clarification when:
  * The user refers to a system, integration, or operation by an incomplete or ambiguous name, and a tool search would return too many equally plausible matches
  * The user asks to generate an IDS but has not specified which systems or operations are involved (e.g. "generate an IDS for our CRM integration" with no further detail)
  * Multiple valid interpretations exist and the answer would differ significantly between them
- Do NOT use ask_clarification when:
  * A search_rest_api_operations call can resolve the ambiguity — try the search first
  * The request is clear enough to give a useful answer even if some details are missing
  * You are being cautious rather than genuinely uncertain
- Ask at most ONE question per turn. Make it specific and actionable so the user knows exactly what you need.

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

// makeAskClarificationTool returns the LLM tool definition for the clarification meta-tool.
// The backend intercepts calls to this tool and converts the question into the final assistant
// response without forwarding results back to the model — the loop terminates immediately.
func makeAskClarificationTool() LLMTool {
	return LLMTool{
		Name: toolNameAskClarification,
		Description: "Ask the user a single clarifying question when their request is too ambiguous to answer reliably. " +
			"The question you provide will be shown to the user as your response. " +
			"Use this instead of guessing. Do NOT use it when a search could resolve the ambiguity — try searching first.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"question": map[string]interface{}{
					"type":        "string",
					"description": "A specific, concise clarifying question for the user. One question only.",
				},
			},
			"required": []string{"question"},
		},
	}
}

// NewChatService creates a chatServiceImpl wired to the OpenAI Responses API.
// It constructs an openAIChatClient internally; callers outside this package only
// see *chatServiceImpl (an opaque concrete type).
func NewChatService(
	systemInfoService SystemInfoService,
	mcpService MCPService,
	generatedFiles GeneratedFileService,
	mintFileToken FileTokenMinter,
) *chatServiceImpl {
	llm := newOpenAIChatService(systemInfoService)

	mcpTools := mcpService.MakeLLMTools()
	if mcpService.IDSAssetsAvailable() && generatedFiles != nil && mintFileToken != nil {
		mcpTools = append(mcpTools, makeIDSChatTools()...)
	} else {
		log.Info("ai-chat: IDS authoring tools NOT exposed (assets/services missing)")
	}
	mcpTools = append(mcpTools, makeAskClarificationTool())

	log.Infof("ChatService initialized with %d LLM tools", len(mcpTools))
	for _, tool := range mcpTools {
		log.Debugf("LLM tool available: %s - %s", tool.Name, tool.Description)
	}

	return &chatServiceImpl{
		systemInfoService: systemInfoService,
		mcpService:        mcpService,
		generatedFiles:    generatedFiles,
		mintFileToken:     mintFileToken,
		llmProvider:       llm,
		mcpTools:          mcpTools,
	}
}

type chatServiceImpl struct {
	systemInfoService SystemInfoService
	mcpService        MCPService
	generatedFiles    GeneratedFileService
	mintFileToken     FileTokenMinter

	llmProvider LLMProvider
	mcpTools    []LLMTool

	// Cache for api-packages-list resource (invalidated after 24 h).
	packagesListCache struct {
		mu        sync.RWMutex
		data      string
		expiresAt time.Time
	}
}

// ModelContextWindow exposes the model's token budget to AiChatService for compaction.
func (c *chatServiceImpl) ModelContextWindow() int {
	return c.llmProvider.ContextWindowSize()
}

// generateChatTitle asks the LLM for a short title; delegates to the LLM client.
func (c *chatServiceImpl) generateChatTitle(ctx context.Context, userText, assistantText string) string {
	return c.llmProvider.GenerateTitle(ctx, userText, assistantText)
}

// summarizeMessagesForCompaction delegates to the LLM client.
func (c *chatServiceImpl) summarizeMessagesForCompaction(ctx context.Context, prior *string, msgs []ChatMessage) string {
	return c.llmProvider.SummarizeForCompaction(ctx, prior, msgs)
}

// truncateRunes returns at most n runes from s, appending "…" if truncated.
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n]) + "…"
}

// toolCallRecord pairs a provider tool-call id with the UI invocation summary.
// Used to emit tool.completed SSE events after local execution.
type toolCallRecord struct {
	ToolCallID string
	Inv        view.AiChatToolInvocation
}

// chatTurnResult is the aggregated result of a full multi-step LLM turn
// (possibly spanning several Execute calls due to tool calls).
type chatTurnResult struct {
	AssistantContent   string
	Usage              *ChatUsage
	ToolInvocations    []view.AiChatToolInvocation
	ToolCallRecords    []toolCallRecord
	OpenAICompletionID string // the continuation token of the FINAL response in the chain
}

// chatStreamHooks lets AiChatService react to streaming events as they arrive.
// All callbacks are optional; nil values are silently skipped.
type chatStreamHooks struct {
	// OnTextDelta is invoked for every incremental text chunk from the LLM.
	OnTextDelta func(delta string)
	// OnToolStart fires when the model commits to calling a tool (before execution).
	OnToolStart func(callID, name string)
	// OnToolCompleted fires after a tool has been executed locally.
	OnToolCompleted func(rec toolCallRecord)
}

// runChatCompletionWithHistory drives a non-streaming multi-step LLM turn.
//
// previousResponseID:
//   - nil: fresh turn or post-compaction. Full viewMessages slice is sent.
//   - non-nil: carry server-side context via the continuation token; only the
//     latest user message is sent (makes FE→BE→LLM traffic cheap).
//
// The function loops: Execute → execute tool calls → Execute with results, until
// the model produces a final text response or maxIterations is reached.
// OpenAICompletionID in the returned result is the continuation token of the
// FINAL response and should be persisted as ai_chat.openai_previous_response_id.
func (c *chatServiceImpl) runChatCompletionWithHistory(ctx context.Context, viewMessages []ChatMessage, previousResponseID *string) (*chatTurnResult, error) {
	log.Debugf("Chat turn (non-streaming, prev_id=%v, tools=%d)", previousResponseID != nil, len(c.mcpTools))

	systemMsg := c.buildSystemMessage(ctx)
	req := LLMRequest{
		Messages:          viewMessages,
		ContinuationToken: previousResponseID,
		SystemMessage:     systemMsg,
		Tools:             c.mcpTools,
	}
	return c.runToolLoop(ctx, req, false, chatStreamHooks{})
}

// runChatCompletionStreaming is the streaming twin of runChatCompletionWithHistory.
// Text deltas reach the caller incrementally via hooks.OnTextDelta; tool lifecycle
// events are delivered via hooks.OnToolStart and hooks.OnToolCompleted.
func (c *chatServiceImpl) runChatCompletionStreaming(ctx context.Context, viewMessages []ChatMessage, previousResponseID *string, hooks chatStreamHooks) (*chatTurnResult, error) {
	log.Debugf("Chat turn (streaming, prev_id=%v, tools=%d)", previousResponseID != nil, len(c.mcpTools))

	systemMsg := c.buildSystemMessage(ctx)
	req := LLMRequest{
		Messages:          viewMessages,
		ContinuationToken: previousResponseID,
		SystemMessage:     systemMsg,
		Tools:             c.mcpTools,
	}
	return c.runToolLoop(ctx, req, true, hooks)
}

// runToolLoop is the shared tool-call loop used by both streaming and non-streaming paths.
func (c *chatServiceImpl) runToolLoop(ctx context.Context, req LLMRequest, streaming bool, hooks chatStreamHooks) (*chatTurnResult, error) {
	const maxIterations = 10

	var totalUsage ChatUsage
	var allToolInvocations []view.AiChatToolInvocation
	var allToolCallRecords []toolCallRecord
	var assistantText strings.Builder
	var lastToken string

	for iteration := 0; iteration < maxIterations; iteration++ {
		var resp *LLMResponse
		var err error

		if streaming {
			resp, err = c.llmProvider.ExecuteStreaming(ctx, req, hooks.OnTextDelta, hooks.OnToolStart)
		} else {
			resp, err = c.llmProvider.Execute(ctx, req)
		}
		if err != nil {
			return nil, err
		}

		assistantText.WriteString(resp.AssistantText)
		lastToken = resp.ContinuationToken
		totalUsage.PromptTokens += resp.Usage.PromptTokens
		totalUsage.CompletionTokens += resp.Usage.CompletionTokens
		totalUsage.TotalTokens += resp.Usage.TotalTokens

		if len(resp.ToolCalls) == 0 {
			log.Debugf("Tool loop done after %d iteration(s) (streaming=%v)", iteration+1, streaming)
			break
		}

		// ask_clarification is a meta-tool: the model's question becomes the final
		// assistant response and the loop terminates without a follow-up LLM call.
		if question, isClarification := extractClarificationQuestion(resp.ToolCalls); isClarification {
			log.Debugf("Model requested clarification: %q", truncateRunes(question, 120))
			assistantText.WriteString(question)
			lastToken = resp.ContinuationToken
			if streaming && hooks.OnTextDelta != nil {
				hooks.OnTextDelta(question)
			}
			break
		}

		toolResultStrs, invocations, recs, err := c.executeToolCallsWithInvocations(ctx, resp.ToolCalls)
		if err != nil {
			log.Errorf("Tool execution failed: %v", err)
			toolResultStrs = make([]string, len(resp.ToolCalls))
		}
		allToolInvocations = append(allToolInvocations, invocations...)
		allToolCallRecords = append(allToolCallRecords, recs...)

		if streaming && hooks.OnToolCompleted != nil {
			for _, rec := range recs {
				hooks.OnToolCompleted(rec)
			}
		}

		llmResults := make([]LLMToolResult, len(resp.ToolCalls))
		for i, tc := range resp.ToolCalls {
			llmResults[i] = LLMToolResult{ToolCallID: tc.ID, Result: toolResultStrs[i]}
		}
		token := resp.ContinuationToken
		req = LLMRequest{
			ToolResults:       llmResults,
			ContinuationToken: &token,
			SystemMessage:     req.SystemMessage,
			Tools:             req.Tools,
		}

		if iteration == maxIterations-1 {
			return nil, fmt.Errorf("reached maximum iterations (%d) without final response", maxIterations)
		}
	}

	usage := totalUsage
	return &chatTurnResult{
		AssistantContent:   assistantText.String(),
		OpenAICompletionID: lastToken,
		ToolInvocations:    allToolInvocations,
		ToolCallRecords:    allToolCallRecords,
		Usage:              &usage,
	}, nil
}

// buildSystemMessage assembles the system prompt, optionally injecting the
// api-packages-list resource from the configured MCP workspace. The resource
// content is cached for 24 h to avoid hammering the DB on every turn.
func (c *chatServiceImpl) buildSystemMessage(ctx context.Context) string {
	mcpWorkspace := c.systemInfoService.GetAiMCPConfig().Workspace
	if mcpWorkspace == "" {
		return systemMessageBaseContent
	}

	c.packagesListCache.mu.RLock()
	cachedData := c.packagesListCache.data
	cacheExpired := time.Now().After(c.packagesListCache.expiresAt)
	c.packagesListCache.mu.RUnlock()

	if cachedData != "" && !cacheExpired {
		log.Debugf("Using cached api-packages-list resource (expires at: %v)", c.packagesListCache.expiresAt)
		return systemMessageBaseContent + "\n\nCURRENT WORKSPACE PACKAGES (from api-packages-list resource):\n" + cachedData
	}

	log.Debugf("Cache expired or empty, fetching fresh api-packages-list resource")
	resourceContents, err := c.mcpService.GetPackagesList(ctx, mcpWorkspace)
	if err != nil {
		log.Warnf("Failed to read api-packages-list resource: %v", err)
		if cachedData != "" {
			log.Debugf("Using expired cache as fallback")
			return systemMessageBaseContent + "\n\nCURRENT WORKSPACE PACKAGES (from api-packages-list resource):\n" + cachedData
		}
		return systemMessageBaseContent
	}

	var resourceData string
	if len(resourceContents) > 0 {
		if textContent, ok := resourceContents[0].(*mcpgo.TextResourceContents); ok {
			resourceData = textContent.Text
		}
	}

	if resourceData != "" {
		c.packagesListCache.mu.Lock()
		c.packagesListCache.data = resourceData
		c.packagesListCache.expiresAt = time.Now().Add(24 * time.Hour)
		c.packagesListCache.mu.Unlock()
		log.Debugf("Updated api-packages-list cache (expires at: %v)", c.packagesListCache.expiresAt)
		return systemMessageBaseContent + "\n\nCURRENT WORKSPACE PACKAGES (from api-packages-list resource):\n" + resourceData
	}
	return systemMessageBaseContent
}

// extractClarificationQuestion checks whether the tool calls contain an ask_clarification
// invocation and, if so, returns the question text and true. The model may include other
// tool calls alongside it; we handle the clarification first and ignore the rest.
func extractClarificationQuestion(toolCalls []LLMToolCall) (string, bool) {
	for _, tc := range toolCalls {
		if tc.Name != toolNameAskClarification {
			continue
		}
		var args struct {
			Question string `json:"question"`
		}
		if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil {
			log.Warnf("ask_clarification: failed to parse arguments: %v", err)
			return "", false
		}
		q := strings.TrimSpace(args.Question)
		if q == "" {
			log.Warn("ask_clarification: empty question in tool arguments")
			return "", false
		}
		return q, true
	}
	return "", false
}

// executeToolCallsWithInvocations runs MCP tools requested by the model.
// Returns: result strings (same length as toolCalls), view invocations, streaming records.
func (c *chatServiceImpl) executeToolCallsWithInvocations(ctx context.Context, toolCalls []LLMToolCall) ([]string, []view.AiChatToolInvocation, []toolCallRecord, error) {
	results := make([]string, len(toolCalls))
	invocations := make([]view.AiChatToolInvocation, 0, len(toolCalls))
	records := make([]toolCallRecord, 0, len(toolCalls))

	for i, toolCall := range toolCalls {
		log.Debugf("Executing tool call: %s with args: %s", toolCall.Name, toolCall.Arguments)
		started := time.Now()

		var args map[string]interface{}
		if err := json.Unmarshal([]byte(toolCall.Arguments), &args); err != nil {
			log.Errorf("Failed to parse tool arguments: %v", err)
			results[i] = fmt.Sprintf("Error parsing arguments: %v", err)
			ms := int(time.Since(started).Milliseconds())
			inv := view.AiChatToolInvocation{Name: toolCall.Name, Status: "error", DurationMs: &ms}
			invocations = append(invocations, inv)
			records = append(records, toolCallRecord{ToolCallID: toolCall.ID, Inv: inv})
			continue
		}

		argsBytes, _ := json.Marshal(args)
		mcpReqWrapper := view.MCPToolRequestWrapper{Name: toolCall.Name, Arguments: argsBytes}
		mcpReq := mcpReqWrapper.ToCallToolRequest()

		var result *mcpgo.CallToolResult
		var err error
		switch toolCall.Name {
		case "search_rest_api_operations":
			result, err = c.mcpService.ExecuteSearchTool(ctx, mcpReq)
		case "get_rest_api_operations_specification":
			result, err = c.mcpService.ExecuteGetSpecTool(ctx, mcpReq)
		case toolNameStartIDSGeneration:
			result, err = c.executeStartIDSGeneration(ctx, args)
		case toolNameSaveGeneratedFile:
			result, err = c.executeSaveGeneratedFile(ctx, args)
		default:
			results[i] = fmt.Sprintf("Unknown tool: %s", toolCall.Name)
			ms := int(time.Since(started).Milliseconds())
			inv := view.AiChatToolInvocation{Name: toolCall.Name, Status: "error", DurationMs: &ms}
			invocations = append(invocations, inv)
			records = append(records, toolCallRecord{ToolCallID: toolCall.ID, Inv: inv})
			continue
		}

		ms := int(time.Since(started).Milliseconds())
		if err != nil {
			log.Errorf("MCP tool execution failed: %v", err)
			results[i] = fmt.Sprintf("Error: %v", err)
			inv := view.AiChatToolInvocation{Name: toolCall.Name, Status: "error", DurationMs: &ms}
			invocations = append(invocations, inv)
			records = append(records, toolCallRecord{ToolCallID: toolCall.ID, Inv: inv})
			continue
		}

		status := "ok"
		if result != nil && result.IsError {
			status = "error"
		}
		inv := view.AiChatToolInvocation{Name: toolCall.Name, Status: status, DurationMs: &ms}
		if result != nil {
			invocations = append(invocations, inv)
			records = append(records, toolCallRecord{ToolCallID: toolCall.ID, Inv: inv})
		}

		if result == nil {
			continue
		}

		var resultLogBytes []byte
		resultLogBytes, _ = json.Marshal(result.Content)
		log.Debugf("MCP tool %s returned result (IsError=%v, Content length=%d): %s",
			toolCall.Name, result.IsError, len(result.Content), string(resultLogBytes))

		if result.IsError {
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
			resultJSON, err := json.Marshal(result.Content)
			if err != nil {
				log.Errorf("Failed to marshal tool result: %v", err)
				results[i] = fmt.Sprintf("Error marshaling result: %v", err)
			} else {
				results[i] = string(resultJSON)
				log.Debugf("Tool %s executed successfully, result length: %d", toolCall.Name, len(results[i]))
			}
		}
	}

	return results, invocations, records, nil
}
