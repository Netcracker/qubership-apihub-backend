package midldleware

import (
	"context"
	"net/http"
	"time"

	"github.com/gorilla/mux"
)

// RequestTimeoutMiddleware caps request processing time by wrapping the request context with a
// deadline. It is a cooperative safety net: only context-aware work (go-pg queries, auth, outbound
// clients that accept a context) is canceled when the deadline fires. When the deadline is exceeded
// the downstream operation returns an error that propagates to the handler as a normal failure;
//
// A non-positive timeout disables the cap. MCP and AI chat turn endpoints (both the streaming
// .../messages/stream and the non-streaming POST .../messages) are exempt: a chat turn is an LLM
// tool-loop bounded by its own turn budget, and those endpoints sit under a more generous nginx tier.
func RequestTimeoutMiddleware(timeout time.Duration) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if timeout <= 0 || isMCPPath(r.URL.Path) || (r.Method == http.MethodPost && isAiChatTurnPath(r.URL.Path)) {
				next.ServeHTTP(w, r)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
