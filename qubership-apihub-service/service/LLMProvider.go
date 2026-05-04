package service

import "context"

// LLMProvider abstracts the concrete LLM backend (OpenAI, Anthropic, etc.).
//
// A single Execute / ExecuteStreaming call represents ONE API round-trip.
// The tool-call loop (iterate until no tool calls remain) is driven by
// chatServiceImpl so that tool execution, compaction, and history logic are
// provider-agnostic and live in one place.
type LLMProvider interface {
	// Execute sends a non-streaming request and returns the model's response.
	// When LLMResponse.ToolCalls is non-empty the caller must execute those
	// tools and feed results back via a subsequent Execute call with
	// LLMRequest.ToolResults populated and LLMRequest.ContinuationToken set.
	Execute(ctx context.Context, req LLMRequest) (*LLMResponse, error)

	// ExecuteStreaming sends the same request in streaming mode.
	// onDelta is called for every text chunk as it arrives from the provider.
	// onToolStart is called as soon as the model commits to a function call
	// (before arguments are fully assembled), enabling optimistic UI feedback
	// ("Searching API operations…" appears within ~100 ms of the model deciding).
	// Returns the same LLMResponse shape as Execute; the caller uses it to
	// continue the tool loop.
	ExecuteStreaming(
		ctx context.Context,
		req LLMRequest,
		onDelta func(delta string),
		onToolStart func(callID, name string),
	) (*LLMResponse, error)

	// GenerateTitle produces a ≤6-word title for a conversation turn.
	// Returns "" on any error; callers should keep the existing title.
	GenerateTitle(ctx context.Context, userText, assistantText string) string

	// SummarizeForCompaction condenses older messages into a compact summary
	// that replaces them on the next LLM turn to stay within the context window.
	// On error returns the prior summary unchanged (or "" if nil).
	SummarizeForCompaction(ctx context.Context, prior *string, msgs []ChatMessage) string

	// ContextWindowSize returns the model's maximum input+output token budget.
	ContextWindowSize() int
}

// LLMTool describes a function the model may call.
// Parameters holds a JSON-Schema-compatible object; the concrete LLM client
// is responsible for translating it to the provider's wire format.
type LLMTool struct {
	Name        string
	Description string
	Parameters  map[string]interface{} // JSON Schema object
}

// LLMRequest is the input for a single LLM API call.
// Exactly one of Messages (first turn) or ToolResults (subsequent turn) must be non-empty.
type LLMRequest struct {
	// Messages for the first turn in a chain (nil when sending tool results).
	Messages []ChatMessage
	// ToolResults for subsequent turns responding to ToolCalls from a prior LLMResponse.
	ToolResults []LLMToolResult
	// ContinuationToken carries provider-specific conversation state from the previous
	// Execute call (e.g. OpenAI previous_response_id). nil = start a fresh conversation.
	ContinuationToken *string
	// SystemMessage is always sent even when ContinuationToken is set because the
	// OpenAI Responses API does not carry the Instructions field across response chains.
	SystemMessage string
	// Tools available to the model for this request.
	Tools []LLMTool
}

// LLMToolResult is a single tool execution result to feed back to the model.
type LLMToolResult struct {
	ToolCallID string // must match LLMToolCall.ID from the previous LLMResponse
	Result     string
}

// LLMResponse is the output of a single LLM API call.
type LLMResponse struct {
	// AssistantText is the model's text output for this round-trip.
	// May be non-empty even when ToolCalls is also non-empty (rare but possible).
	AssistantText string
	// ToolCalls are the function calls the model wants to make.
	// Empty means this is the final assistant turn; the loop should terminate.
	ToolCalls []LLMToolCall
	// ContinuationToken is an opaque provider state identifier to pass as
	// LLMRequest.ContinuationToken on the next call in this chain.
	ContinuationToken string
	// Usage for this single API call; the tool loop accumulates these.
	Usage ChatUsage
}

// LLMToolCall is a single function call requested by the model.
type LLMToolCall struct {
	ID        string // provider-assigned correlation id (must round-trip via LLMToolResult.ToolCallID)
	Name      string
	Arguments string // JSON-encoded argument map
}
