package service

import "context"

// aiChatCorrelationIDKey threads a per-turn correlation id through the call stack
// without polluting every function signature. We generate one fresh id per LLM
// turn at the AiChatService entry point, attach it to the context, and then both
//   - propagate it to OpenAI as the X-Request-ID header (so OpenAI's server-side
//     traces can be correlated with our logs during incident triage), and
//   - include it in our own structured log lines.
type aiChatCorrelationIDKey struct{}

// WithAiChatCorrelationID attaches a correlation id to ctx.
func WithAiChatCorrelationID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, aiChatCorrelationIDKey{}, id)
}

// AiChatCorrelationIDFromContext returns the correlation id previously stored
// by WithAiChatCorrelationID, or empty string when missing.
func AiChatCorrelationIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(aiChatCorrelationIDKey{}).(string); ok {
		return v
	}
	return ""
}

// aiChatTurnKey threads the user/chat identity of the current turn through the
// call stack so MCP tool handlers (specifically save_generated_file) can mint
// per-user files without taking those values as explicit arguments. ChatService
// itself stays user-agnostic; only AiChatService.runLLMTurn populates this.
type aiChatTurnKey struct{}

// AiChatTurn captures who the current LLM turn belongs to.
type AiChatTurn struct {
	UserID string
	ChatID string
}

// WithAiChatTurn attaches user/chat identity to ctx for the duration of an LLM turn.
func WithAiChatTurn(ctx context.Context, userID, chatID string) context.Context {
	if userID == "" && chatID == "" {
		return ctx
	}
	return context.WithValue(ctx, aiChatTurnKey{}, AiChatTurn{UserID: userID, ChatID: chatID})
}

// AiChatTurnFromContext returns (turn, true) when one was attached, zero-value+false otherwise.
func AiChatTurnFromContext(ctx context.Context) (AiChatTurn, bool) {
	if ctx == nil {
		return AiChatTurn{}, false
	}
	v, ok := ctx.Value(aiChatTurnKey{}).(AiChatTurn)
	return v, ok
}
