package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/client"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"

	log "github.com/sirupsen/logrus"
)

// openAIChatService implements LLMProvider for the OpenAI Responses API.
// Each Execute / ExecuteStreaming call is a single API round-trip; the tool-call
// loop lives in chatServiceImpl (ChatService.go).
type openAIChatService struct {
	client openai.Client
	sis    SystemInfoService
}

// newOpenAIChatService constructs an openAIChatService.
// It panics on misconfiguration because OpenAI client creation failure is a fatal
// startup error (the service cannot function without the LLM backend).
func newOpenAIChatService(sis SystemInfoService) *openAIChatService {
	cfg := sis.GetAiChatConfig().OpenAI
	c, err := client.NewOpenAIClient(cfg.ApiKey, cfg.ProxyURL)
	if err != nil {
		log.Errorf("Failed to create OpenAI client: %v", err)
		panic(fmt.Sprintf("Failed to create OpenAI client: %v", err))
	}
	return &openAIChatService{client: c, sis: sis}
}

// Execute sends a single non-streaming Responses API request and returns the result.
func (c *openAIChatService) Execute(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
	apiReq := c.buildRequest(req)
	resp, err := c.client.Responses.New(ctx, apiReq, openAIRequestOptions(ctx)...)
	if err != nil {
		logResponsesAPIError(err)
		return nil, fmt.Errorf("OpenAI Responses API: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("nil response from OpenAI Responses API")
	}
	return c.parseOutputItems(
		resp.ID,
		resp.Output,
		int(resp.Usage.InputTokens),
		int(resp.Usage.OutputTokens),
		int(resp.Usage.TotalTokens),
	), nil
}

// ExecuteStreaming sends a single streaming Responses API request.
// onDelta is called for every text delta; onToolStart fires when the model commits
// to a function call (before arguments finish streaming).
func (c *openAIChatService) ExecuteStreaming(
	ctx context.Context,
	req LLMRequest,
	onDelta func(delta string),
	onToolStart func(callID, name string),
) (*LLMResponse, error) {
	apiReq := c.buildRequest(req)
	stream := c.client.Responses.NewStreaming(ctx, apiReq, openAIRequestOptions(ctx)...)

	var result LLMResponse
	startedCalls := make(map[string]bool)

streamLoop:
	for stream.Next() {
		event := stream.Current()
		switch event.Type {
		case "response.output_text.delta":
			if event.Delta != "" {
				result.AssistantText += event.Delta
				if onDelta != nil {
					onDelta(event.Delta)
				}
			}

		case "response.output_item.added":
			// Fire onToolStart as soon as the model commits to calling a tool,
			// before all arguments have streamed in.
			if event.Item.Type == "function_call" {
				fc := event.Item.AsFunctionCall()
				if !startedCalls[fc.CallID] {
					startedCalls[fc.CallID] = true
					if onToolStart != nil {
						onToolStart(fc.CallID, fc.Name)
					}
				}
			}

		case "response.output_item.done":
			if event.Item.Type == "function_call" {
				fc := event.Item.AsFunctionCall()
				// Defensive: fire onToolStart if the "added" event wasn't observed.
				if !startedCalls[fc.CallID] {
					startedCalls[fc.CallID] = true
					if onToolStart != nil {
						onToolStart(fc.CallID, fc.Name)
					}
				}
				result.ToolCalls = append(result.ToolCalls, LLMToolCall{
					ID:        fc.CallID,
					Name:      fc.Name,
					Arguments: fc.Arguments,
				})
			}

		case "response.completed":
			result.ContinuationToken = event.Response.ID
			result.Usage = ChatUsage{
				PromptTokens:     int(event.Response.Usage.InputTokens),
				CompletionTokens: int(event.Response.Usage.OutputTokens),
				TotalTokens:      int(event.Response.Usage.TotalTokens),
			}
			break streamLoop

		case "response.failed":
			if err := stream.Err(); err != nil {
				logResponsesAPIError(err)
			}
			_ = stream.Close()
			return nil, fmt.Errorf("response.failed event from Responses API")

		case "error":
			_ = stream.Close()
			return nil, fmt.Errorf("stream error event: code=%s message=%s", event.Code, event.Message)
		}
	}

	if err := stream.Err(); err != nil {
		_ = stream.Close()
		logResponsesAPIError(err)
		return nil, fmt.Errorf("streaming Responses API: %w", err)
	}
	_ = stream.Close()

	if result.ContinuationToken == "" {
		return nil, fmt.Errorf("stream ended without response.completed")
	}
	return &result, nil
}

// GenerateTitle produces a ≤6-word conversation title via a one-shot Responses call
// (Store=false, no tools, not part of the user-visible chain).
func (c *openAIChatService) GenerateTitle(ctx context.Context, userText, assistantText string) string {
	const sysPrompt = `You write very short (no more than 6 words) chat titles.
Return ONLY the title text - no quotes, no markdown, no trailing punctuation.
Capture the main topic or task of the conversation.`
	prompt := "User asked: " + truncateRunes(userText, 600) + "\n\nAssistant replied: " + truncateRunes(assistantText, 600)

	req := responses.ResponseNewParams{
		Model:        shared.ResponsesModel(c.sis.GetAiChatConfig().OpenAI.Model),
		Instructions: openai.String(sysPrompt),
		Input:        responses.ResponseNewParamsInputUnion{OfString: openai.String(prompt)},
		Store:        openai.Bool(false),
	}
	resp, err := c.client.Responses.New(ctx, req, openAIRequestOptions(ctx)...)
	if err != nil || resp == nil {
		log.Warnf("ai-chat: GenerateTitle OpenAI call failed: %v", err)
		return ""
	}
	title := strings.TrimSpace(resp.OutputText())
	title = strings.Trim(title, "\"'`")
	if len(title) > 80 {
		title = strings.TrimSpace(string([]rune(title)[:80]))
	}
	return title
}

// SummarizeForCompaction condenses an older message slice into a compact summary
// via a one-shot Responses call (Store=false, no tools).
func (c *openAIChatService) SummarizeForCompaction(ctx context.Context, prior *string, msgs []ChatMessage) string {
	if len(msgs) == 0 {
		if prior != nil {
			return *prior
		}
		return ""
	}
	const sysPrompt = `You are summarizing the EARLIER part of an ongoing assistant conversation.
The summary will be substituted for these messages on the next turn so the model still has the relevant context.
Capture concretely:
	- the user's overall goal and constraints
	- decisions already made and facts already established
	- any package/version/operation IDs and tool results that the user is still working with
Return plain text, 4-12 sentences, no markdown headings.`
	var b strings.Builder
	if prior != nil && *prior != "" {
		b.WriteString("Existing summary so far:\n")
		b.WriteString(*prior)
		b.WriteString("\n\n---\n\n")
	}
	b.WriteString("New conversation slice to integrate:\n")
	for _, m := range msgs {
		role := m.Role
		if role == "" {
			role = "user"
		}
		b.WriteString("[")
		b.WriteString(role)
		b.WriteString("] ")
		b.WriteString(truncateRunes(m.Content, 4000))
		b.WriteString("\n")
	}
	req := responses.ResponseNewParams{
		Model:        shared.ResponsesModel(c.sis.GetAiChatConfig().OpenAI.Model),
		Instructions: openai.String(sysPrompt),
		Input:        responses.ResponseNewParamsInputUnion{OfString: openai.String(b.String())},
		Store:        openai.Bool(false),
	}
	resp, err := c.client.Responses.New(ctx, req, openAIRequestOptions(ctx)...)
	if err != nil || resp == nil {
		log.Warnf("ai-chat: SummarizeForCompaction failed: %v", err)
		if prior != nil {
			return *prior
		}
		return ""
	}
	return strings.TrimSpace(resp.OutputText())
}

// ContextWindowSize returns the maximum token budget for the configured model.
func (c *openAIChatService) ContextWindowSize() int {
	return modelContextWindow(c.sis.GetAiChatConfig().OpenAI.Model)
}

// modelContextWindow maps known model names to their approximate context windows.
// Conservative estimates are preferred: triggering compaction a little early
// is much better than hitting a token-limit error mid-turn.
func modelContextWindow(model string) int {
	switch model {
	case "gpt-4o", "gpt-4o-mini", "gpt-4.1", "gpt-4.1-mini", "gpt-4.1-nano",
		"o3-mini", "o4-mini", "gpt-5-mini", "gpt-5":
		return 128000
	case "gpt-4-turbo", "gpt-4-turbo-preview":
		return 128000
	case "gpt-4-32k":
		return 32000
	case "gpt-3.5-turbo", "gpt-3.5-turbo-16k":
		return 16000
	default:
		return 128000
	}
}

// buildRequest translates LLMRequest into a responses.ResponseNewParams.
func (c *openAIChatService) buildRequest(req LLMRequest) responses.ResponseNewParams {
	cfg := c.sis.GetAiChatConfig().OpenAI

	var input responses.ResponseNewParamsInputUnion
	if len(req.ToolResults) > 0 {
		items := make([]responses.ResponseInputItemUnionParam, 0, len(req.ToolResults))
		for _, tr := range req.ToolResults {
			items = append(items, responses.ResponseInputItemParamOfFunctionCallOutput(tr.ToolCallID, tr.Result))
		}
		input = responses.ResponseNewParamsInputUnion{OfInputItemList: items}
	} else {
		// Strip system messages: they go in Instructions, not the input list.
		filtered := make([]ChatMessage, 0, len(req.Messages))
		for _, m := range req.Messages {
			if m.Role != "system" {
				filtered = append(filtered, m)
			}
		}
		input = responses.ResponseNewParamsInputUnion{
			OfInputItemList: convertToInputItems(filtered),
		}
	}

	apiReq := responses.ResponseNewParams{
		Model:        shared.ResponsesModel(cfg.Model),
		Instructions: openai.String(req.SystemMessage),
		Input:        input,
		Tools:        convertToResponsesTools(req.Tools),
		Temperature:  openai.Float(cfg.Temperature),
		Reasoning:    shared.ReasoningParam{Effort: convertReasoningEffort(cfg.ReasoningEffort)},
		Text:         responses.ResponseTextConfigParam{Verbosity: convertVerbosity(cfg.Verbosity)},
		Store:        openai.Bool(true),
	}
	if req.ContinuationToken != nil && *req.ContinuationToken != "" {
		apiReq.PreviousResponseID = openai.String(*req.ContinuationToken)
	}
	return apiReq
}

// parseOutputItems extracts text and tool calls from a Responses API output list.
func (c *openAIChatService) parseOutputItems(
	responseID string,
	output []responses.ResponseOutputItemUnion,
	inputTokens, outputTokens, totalTokens int,
) *LLMResponse {
	result := &LLMResponse{
		ContinuationToken: responseID,
		Usage: ChatUsage{
			PromptTokens:     inputTokens,
			CompletionTokens: outputTokens,
			TotalTokens:      totalTokens,
		},
	}
	for _, out := range output {
		switch out.Type {
		case "message":
			msg := out.AsMessage()
			for _, content := range msg.Content {
				if content.Type == "output_text" {
					result.AssistantText += content.Text
				}
			}
		case "function_call":
			fc := out.AsFunctionCall()
			result.ToolCalls = append(result.ToolCalls, LLMToolCall{
				ID:        fc.CallID,
				Name:      fc.Name,
				Arguments: fc.Arguments,
			})
		default:
			log.Debugf("Ignoring Responses API output item of type %q", out.Type)
		}
	}
	return result
}

// convertToInputItems converts a ChatMessage slice to Responses API input items.
// System messages should already be stripped by the caller (they go in Instructions).
func convertToInputItems(messages []ChatMessage) []responses.ResponseInputItemUnionParam {
	result := make([]responses.ResponseInputItemUnionParam, 0, len(messages))
	for _, msg := range messages {
		role := responses.EasyInputMessageRoleUser
		switch msg.Role {
		case "assistant":
			role = responses.EasyInputMessageRoleAssistant
		case "system":
			role = responses.EasyInputMessageRoleDeveloper
		}
		result = append(result, responses.ResponseInputItemUnionParam{
			OfMessage: &responses.EasyInputMessageParam{
				Role: role,
				Content: responses.EasyInputMessageContentUnionParam{
					OfString: openai.String(msg.Content),
				},
			},
		})
	}
	return result
}

// convertToResponsesTools converts LLMTool descriptors to the Responses API wire format.
// Strict mode is left false because MCP-provided JSON schemas are not always strict-compatible.
func convertToResponsesTools(tools []LLMTool) []responses.ToolUnionParam {
	result := make([]responses.ToolUnionParam, 0, len(tools))
	for _, tool := range tools {
		params := map[string]any{}
		for k, v := range tool.Parameters {
			params[k] = v
		}
		result = append(result, responses.ToolUnionParam{
			OfFunction: &responses.FunctionToolParam{
				Name:        tool.Name,
				Description: openai.String(tool.Description),
				Parameters:  params,
				Strict:      openai.Bool(false),
			},
		})
	}
	return result
}

// convertReasoningEffort maps the config string to the Responses API enum.
func convertReasoningEffort(value string) shared.ReasoningEffort {
	switch value {
	case "minimal":
		return shared.ReasoningEffortMinimal
	case "low":
		return shared.ReasoningEffortLow
	case "medium":
		return shared.ReasoningEffortMedium
	case "high":
		return shared.ReasoningEffortHigh
	default:
		return shared.ReasoningEffortMedium
	}
}

// convertVerbosity maps the config string to the Responses API enum.
func convertVerbosity(value string) responses.ResponseTextConfigVerbosity {
	switch value {
	case "low":
		return responses.ResponseTextConfigVerbosityLow
	case "medium":
		return responses.ResponseTextConfigVerbosityMedium
	case "high":
		return responses.ResponseTextConfigVerbosityHigh
	default:
		return responses.ResponseTextConfigVerbosityMedium
	}
}

// openAIRequestOptions returns per-call RequestOption slice.
// Currently injects the AI-chat correlation id as X-Request-ID so OpenAI
// server-side traces can be aligned with our own structured log fields.
func openAIRequestOptions(ctx context.Context) []option.RequestOption {
	corrID := AiChatCorrelationIDFromContext(ctx)
	if corrID == "" {
		return nil
	}
	return []option.RequestOption{option.WithHeader("X-Request-ID", corrID)}
}

// logResponsesAPIError logs structured details from an OpenAI API error.
func logResponsesAPIError(err error) {
	var apiErr *openai.Error
	if errors.As(err, &apiErr) {
		log.WithFields(log.Fields{
			"error_type":     apiErr.Type,
			"error_code":     apiErr.Code,
			"error_message":  apiErr.Message,
			"error_param":    apiErr.Param,
			"status_code":    apiErr.StatusCode,
			"request_url":    apiErr.Request.URL.String(),
			"request_method": apiErr.Request.Method,
			"raw_json":       apiErr.RawJSON(),
		}).Errorf("OpenAI Responses API error: %s %s returned status %d - %s (code: %s, param: %s)",
			apiErr.Request.Method, apiErr.Request.URL.String(),
			apiErr.StatusCode, apiErr.Message, apiErr.Code, apiErr.Param)
		if log.IsLevelEnabled(log.DebugLevel) {
			log.Debugf("OpenAI request details:\n%s", string(apiErr.DumpRequest(true)))
			log.Debugf("OpenAI response details:\n%s", string(apiErr.DumpResponse(true)))
		}
		return
	}
	log.WithError(err).Errorf("OpenAI Responses API non-API error: %v", err)
}

// IsInvalidPrevResponseIDError reports whether err is an OpenAI rejection of our
// previous_response_id. Used by AiChatService to trigger a full-history retry.
//
// This normally happens because:
//   - the previously stored response has aged out of OpenAI's response store;
//   - the response belongs to a different project than the current API key;
//   - the stored id was corrupted in our DB.
func IsInvalidPrevResponseIDError(err error) bool {
	var apiErr *openai.Error
	if !errors.As(err, &apiErr) {
		return false
	}
	if apiErr.Param == "previous_response_id" {
		return true
	}
	if apiErr.StatusCode == 404 {
		msg := strings.ToLower(apiErr.Message)
		if strings.Contains(msg, "response") && strings.Contains(msg, "not found") {
			return true
		}
	}
	if apiErr.StatusCode == 400 {
		msg := strings.ToLower(apiErr.Message)
		if strings.Contains(msg, "previous_response_id") || strings.Contains(msg, "previous response") {
			return true
		}
	}
	return false
}

// Compile-time check: openAIChatService must satisfy LLMProvider.
var _ LLMProvider = (*openAIChatService)(nil)
