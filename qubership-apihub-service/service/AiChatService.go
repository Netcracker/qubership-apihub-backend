package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/entity"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/exception"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/metrics"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/repository"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/utils"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
)

// minRecentMessagesAfterCompaction is the smallest number of trailing messages we always keep
// verbatim in the prompt after compaction (so the model still has fresh exact wording).
const minRecentMessagesAfterCompaction = 8

const (
	// MaxAiPinnedChatsPerUser is the hardcoded limit on pinned chats per user (mirrored on the FE).
	MaxAiPinnedChatsPerUser = 3
	// MaxAiUserMessageRunes is the hardcoded maximum message length in runes (mirrored on the FE).
	MaxAiUserMessageRunes = 32000
	// maxAiContextMessages caps the number of messages fetched from DB to build the LLM history.
	maxAiContextMessages = 200
)

// AiChatService is the productized /api/v1/ai-chat/* backend
type AiChatService interface {
	ListChats(ctx context.Context, userID, search string, before *time.Time, limit int) (*view.AiChatsListResponse, error)
	CreateChat(ctx context.Context, userID string, title *string) (*view.AiChat, error)
	GetChat(ctx context.Context, userID, chatID string) (*view.AiChat, error)
	UpdateChat(ctx context.Context, userID, chatID string, req *view.AiChatUpdateRequest) (*view.AiChat, error)
	DeleteChat(ctx context.Context, userID, chatID string) error
	ListMessages(ctx context.Context, userID, chatID string, before *time.Time, limit int) (*view.AiChatMessagesListResponse, error)
	SendMessage(ctx context.Context, userID, chatID string, req *view.AiChatSendMessageRequest) (*view.AiChatSendMessageResponse, error)
	SendMessageStream(ctx context.Context, userID, chatID string, req *view.AiChatSendMessageRequest) (<-chan AiChatStreamChunk, error)
	// GetFileForUser returns a stored file row for download (after JWT validation in controller)
	GetFileForUser(ctx context.Context, fileID, userID string) (*entity.AiChatFileEntity, error)
}

// AiChatStreamChunk is one SSE data payload
type AiChatStreamChunk struct {
	EventName string
	Data      interface{}
}

// FileTokenMinter issues short-lived JWTs for generated file URLs (injected to avoid import cycles)
type FileTokenMinter func(userID, fileID string, ttl time.Duration) (string, error)

var generatedFileURLPattern = regexp.MustCompile(`(/api/v1/generated-files/[0-9a-fA-F-]{36})(?:\?token=[^)\s"<>]*)?`)

type aiChatServiceImpl struct {
	sis          SystemInfoService
	repo         repository.AiChatRepository
	chat         *chatServiceImpl
	mintFileToken FileTokenMinter
}

// NewAiChatService wires the productized API; chatSvc is the OpenAI chat implementation; mint is typically security.MintGeneratedFileToken
func NewAiChatService(sis SystemInfoService, repo repository.AiChatRepository, chatSvc *chatServiceImpl, mint FileTokenMinter) (AiChatService, error) {
	if mint == nil {
		return nil, fmt.Errorf("file token minter is required")
	}
	return &aiChatServiceImpl{sis: sis, repo: repo, chat: chatSvc, mintFileToken: mint}, nil
}

// resignGeneratedFileLinksInContent refreshes ?token= for each still-valid file row.
// Always produces relative URLs (/api/v1/generated-files/<id>?token=...) so that links
// work on any host/port without depending on the server-side apihubExternalUrl config.
func (s *aiChatServiceImpl) resignGeneratedFileLinksInContent(ctx context.Context, userID, content string) (string, error) {
	return generatedFileURLPattern.ReplaceAllStringFunc(content, func(match string) string {
		parts := generatedFileURLPattern.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}
		path := parts[1]
		id := path[len("/api/v1/generated-files/"):]
		f, err := s.repo.GetFileByIDForUser(ctx, id, userID)
		if err != nil || f == nil {
			return match
		}
		if f.ExpiresAt.Before(time.Now().UTC()) {
			return match
		}
		ttl := time.Until(f.ExpiresAt)
		if ttl <= 0 {
			return match
		}
		tok, err := s.mintFileToken(userID, id, ttl)
		if err != nil {
			return match
		}
		return path + "?token=" + tok
	}), nil
}

func (s *aiChatServiceImpl) GetFileForUser(ctx context.Context, fileID, userID string) (*entity.AiChatFileEntity, error) {
	return s.repo.GetFileByIDForUser(ctx, fileID, userID)
}

func errAiNotFound() *exception.CustomError {
	return &exception.CustomError{Status: 404, Code: "APIHUB-AI-3001", Message: "Not found"}
}

func errAiPinLimit() *exception.CustomError {
	return &exception.CustomError{Status: 400, Code: "APIHUB-AI-4003", Message: "Maximum number of pinned chats reached"}
}


func (s *aiChatServiceImpl) ListChats(ctx context.Context, userID, search string, before *time.Time, limit int) (*view.AiChatsListResponse, error) {
	rows, err := s.repo.ListChats(ctx, repository.AiChatsListFilter{
		UserID: userID,
		Search: search,
		Before: before,
		Limit:  limit + 1,
	})
	if err != nil {
		return nil, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]view.AiChat, 0, len(rows))
	for i := range rows {
		out = append(out, *entityToAiChatView(&rows[i]))
	}
	return &view.AiChatsListResponse{Chats: out, HasMore: hasMore}, nil
}

func (s *aiChatServiceImpl) CreateChat(ctx context.Context, userID string, title *string) (*view.AiChat, error) {
	now := time.Now().UTC()
	id := uuid.NewString()
	t := ""
	if title != nil {
		t = *title
	}
	row := &entity.AiChatEntity{
		ID:            id,
		UserID:        userID,
		Title:         t,
		Pinned:        false,
		CreatedAt:     now,
		LastMessageAt: now,
		MessagesCount: 0,
	}
	if err := s.repo.CreateChat(ctx, row); err != nil {
		return nil, err
	}
	return entityToAiChatView(row), nil
}

func (s *aiChatServiceImpl) GetChat(ctx context.Context, userID, chatID string) (*view.AiChat, error) {
	ch, err := s.repo.GetChatByIDForUser(ctx, chatID, userID)
	if err != nil {
		return nil, err
	}
	if ch == nil {
		return nil, errAiNotFound()
	}
	return entityToAiChatView(ch), nil
}

func (s *aiChatServiceImpl) UpdateChat(ctx context.Context, userID, chatID string, req *view.AiChatUpdateRequest) (*view.AiChat, error) {
	if req == nil {
		return nil, &exception.CustomError{Status: 400, Code: exception.BadRequestBody, Message: "Empty body"}
	}
	ch, err := s.repo.GetChatByIDForUser(ctx, chatID, userID)
	if err != nil {
		return nil, err
	}
	if ch == nil {
		return nil, errAiNotFound()
	}
	if req.Title != nil {
		ch.Title = *req.Title
	}
	if req.Pinned != nil {
		if *req.Pinned {
			if !ch.Pinned {
				n, err := s.repo.CountPinnedChats(ctx, userID)
				if err != nil {
					return nil, err
				}
				if n >= MaxAiPinnedChatsPerUser {
					return nil, errAiPinLimit()
				}
			}
		}
		ch.Pinned = *req.Pinned
	}
	if err := s.repo.UpdateChat(ctx, ch); err != nil {
		return nil, err
	}
	return entityToAiChatView(ch), nil
}

func (s *aiChatServiceImpl) DeleteChat(ctx context.Context, userID, chatID string) error {
	n, err := s.repo.DeleteChat(ctx, chatID, userID)
	if err != nil {
		return err
	}
	if n == 0 {
		return errAiNotFound()
	}
	return nil
}

func (s *aiChatServiceImpl) ListMessages(ctx context.Context, userID, chatID string, before *time.Time, limit int) (*view.AiChatMessagesListResponse, error) {
	if _, err := s.mustGetChat(ctx, userID, chatID); err != nil {
		return nil, err
	}
	rows, err := s.repo.ListMessages(ctx, repository.AiMessagesListFilter{
		ChatID: chatID,
		Before: before,
		Limit:  limit + 1,
	})
	if err != nil {
		return nil, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]view.AiChatMessage, 0, len(rows))
	for i := range rows {
		vm, e := s.entityToMessageView(ctx, userID, &rows[i])
		if e != nil {
			return nil, e
		}
		out = append(out, *vm)
	}
	return &view.AiChatMessagesListResponse{Messages: out, HasMore: hasMore}, nil
}

func (s *aiChatServiceImpl) mustGetChat(ctx context.Context, userID, chatID string) (*entity.AiChatEntity, error) {
	ch, err := s.repo.GetChatByIDForUser(ctx, chatID, userID)
	if err != nil {
		return nil, err
	}
	if ch == nil {
		return nil, errAiNotFound()
	}
	return ch, nil
}

// SendMessage runs a full LLM turn and persists result
func (s *aiChatServiceImpl) SendMessage(ctx context.Context, userID, chatID string, req *view.AiChatSendMessageRequest) (*view.AiChatSendMessageResponse, error) {
	chat, err := s.mustGetChat(ctx, userID, chatID)
	if err != nil {
		return nil, err
	}
	if utf8.RuneCountInString(req.Content) > MaxAiUserMessageRunes {
		return nil, &exception.CustomError{Status: 400, Code: exception.InvalidParameterValue, Message: "Message too long", Params: map[string]interface{}{"param": "content", "max": MaxAiUserMessageRunes}}
	}

	started := time.Now()
	um, am, e := s.runTurn(ctx, userID, chat, req, nil)
	s.observeTurn("sync", started, e)
	if e != nil {
		return nil, e
	}
	return &view.AiChatSendMessageResponse{UserMessage: *um, AssistantMessage: *am}, nil
}

// SendMessageStream returns a channel of SSE payload objects
func (s *aiChatServiceImpl) SendMessageStream(ctx context.Context, userID, chatID string, req *view.AiChatSendMessageRequest) (<-chan AiChatStreamChunk, error) {
	chat, err := s.mustGetChat(ctx, userID, chatID)
	if err != nil {
		return nil, err
	}
	if utf8.RuneCountInString(req.Content) > MaxAiUserMessageRunes {
		return nil, &exception.CustomError{Status: 400, Code: exception.InvalidParameterValue, Message: "Message too long"}
	}

	out := make(chan AiChatStreamChunk, 32)
	go func() {
		defer close(out)
		started := time.Now()
		_, _, err := s.runTurn(ctx, userID, chat, req, out)
		s.observeTurn("stream", started, err)
		if err != nil {
			_ = s.emitStream(out, "error", map[string]interface{}{
				"type":    "error",
				"code":    "APIHUB-AI-5000",
				"message": err.Error(),
			})
		}
	}()
	return out, nil
}

// observeTurn records turn-level Prometheus metrics. Token histogram is updated here too
// when we have access to the last computed usage on the chat row.
func (s *aiChatServiceImpl) observeTurn(mode string, started time.Time, err error) {
	status := "ok"
	if err != nil {
		status = "error"
	}
	metrics.AiChatTurnsTotal.WithLabelValues(mode, status).Inc()
	metrics.AiChatTurnDuration.WithLabelValues(mode, status).Observe(time.Since(started).Seconds())
}

// streamModeForChan picks the metric label "stream" or "sync" based on whether the caller
// passed a stream channel into the turn; helpers below the stream call site use this to label
// per-turn token histograms consistently.
func streamModeForChan(ch chan<- AiChatStreamChunk) string {
	if ch == nil {
		return "sync"
	}
	return "stream"
}

func (s *aiChatServiceImpl) emitStream(ch chan<- AiChatStreamChunk, name string, data interface{}) error {
	if ch == nil {
		return nil
	}
	ch <- AiChatStreamChunk{EventName: name, Data: data}
	return nil
}

// runTurn executes persistence + LLM; if stream is non-nil, also emits SSE
//
// Idempotency contract:
//   - If the user message with the same clientMessageId already exists AND a later assistant
//     message exists, return the cached pair without calling the LLM (and replay SSE if streaming).
//   - If the user message exists but no assistant message follows yet (e.g. previous attempt
//     errored out after persisting user message), the LLM call is retried so the client gets
//     a complete pair instead of a hard 500.
//   - Otherwise insert a fresh user message; concurrent inserters with the same clientMessageId
//     race on the partial unique index and the loser replays the cached pair.
func (s *aiChatServiceImpl) runTurn(ctx context.Context, userID string, chat *entity.AiChatEntity, req *view.AiChatSendMessageRequest, stream chan<- AiChatStreamChunk) (*view.AiChatMessage, *view.AiChatMessage, error) {
	var clientID *string
	if req.ClientMessageID != nil && *req.ClientMessageID != "" {
		c := *req.ClientMessageID
		clientID = &c
	}

	if clientID != nil {
		existing, err := s.repo.FindUserMessageByClientID(ctx, chat.ID, *clientID)
		if err != nil {
			return nil, nil, err
		}
		if existing != nil {
			if a, err := s.repo.FindNextAssistantMessage(ctx, chat.ID, existing.CreatedAt); err != nil {
				return nil, nil, err
			} else if a != nil {
				return s.serveCachedPair(ctx, userID, existing, a, stream)
			}
			return s.runLLMTurn(ctx, userID, chat, existing, stream)
		}
	}

	now := time.Now().UTC()
	um := &entity.AiChatMessageEntity{
		ID:              uuid.NewString(),
		ChatID:          chat.ID,
		Role:            "user",
		Content:         req.Content,
		ClientMessageID: clientID,
		CreatedAt:       now,
	}
	inserted, err := s.repo.TryInsertUserMessageIdempotent(ctx, um)
	if err != nil {
		return nil, nil, err
	}
	if !inserted {
		return s.replayIdempotent(ctx, userID, chat.ID, *clientID, stream)
	}

	return s.runLLMTurn(ctx, userID, chat, um, stream)
}

// runLLMTurn does the part after the user message is guaranteed to exist in DB.
func (s *aiChatServiceImpl) runLLMTurn(ctx context.Context, userID string, chat *entity.AiChatEntity, um *entity.AiChatMessageEntity, stream chan<- AiChatStreamChunk) (*view.AiChatMessage, *view.AiChatMessage, error) {
	// Tag this turn with a correlation id used for both our own structured logs and
	// OpenAI's X-Request-ID header. We do it once per turn (covering the whole
	// tool-call loop on a single user message), not per LLM call.
	if AiChatCorrelationIDFromContext(ctx) == "" {
		ctx = WithAiChatCorrelationID(ctx, uuid.NewString())
	}
	// Make the turn's user/chat identity visible to downstream tool handlers
	// (save_generated_file in particular needs userID/chatID to write files).
	ctx = WithAiChatTurn(ctx, userID, chat.ID)

	hist, err := s.repo.ListMessagesChronological(ctx, chat.ID, maxAiContextMessages)
	if err != nil {
		return nil, nil, err
	}

	compacted := false
	if compactedNow, evt := s.maybeCompactBefore(ctx, chat, hist); compactedNow {
		compacted = true
		metrics.AiChatCompactionsTotal.Inc()
		if stream != nil && evt != nil {
			_ = s.emitStream(stream, "context.compacted", evt)
		}
		log.WithFields(log.Fields{
			"chatId": chat.ID,
			"userId": userID,
		}).Info("ai-chat: context compacted")
	}

	// Decide whether we can ride the Responses API conversation chain via
	// previous_response_id, which lets us send only the latest user message.
	//
	// Conditions for chaining:
	//   - we DID NOT just compact (compaction resets OpenAIPreviousResponseID
	//     to nil so the model re-ingests the new summary on the next turn);
	//   - the chat has a previous_response_id from a prior turn;
	//   - the just-saved user message exists.
	var msgsForLLM []ChatMessage
	var prevResponseID *string
	if !compacted && chat.OpenAIPreviousResponseID != nil && *chat.OpenAIPreviousResponseID != "" && um != nil {
		prevResponseID = chat.OpenAIPreviousResponseID
		msgsForLLM = []ChatMessage{{Role: "user", Content: um.Content}}
	} else {
		msgsForLLM = s.buildHistoryForLLM(chat, hist)
	}

	// Pre-allocate the assistant message id so we can emit message.assistant.start
	// BEFORE the LLM call -- the FE can render a placeholder while we wait for the
	// first delta. The same id ends up in the persisted assistantEnt below.
	assistantID := uuid.NewString()

	if stream != nil {
		_ = s.emitStream(stream, "message.assistant.start", map[string]interface{}{
			"type": "message.assistant.start", "messageId": assistantID,
		})
	}

	// In streaming mode we forward Responses-API events to the SSE channel as they
	// arrive. In non-streaming mode (POST /messages) hooks are nil and we just get
	// the final result as before.
	var hooks chatStreamHooks
	if stream != nil {
		hooks = chatStreamHooks{
			OnTextDelta: func(delta string) {
				_ = s.emitStream(stream, "message.assistant.delta", map[string]interface{}{
					"type": "message.assistant.delta", "delta": delta,
				})
			},
			OnToolStart: func(callID, name string) {
				_ = s.emitStream(stream, "tool.started", map[string]interface{}{
					"type": "tool.started", "toolCallId": callID, "name": name,
				})
			},
			OnToolCompleted: func(rec toolCallRecord) {
				_ = s.emitStream(stream, "tool.completed", map[string]interface{}{
					"type":       "tool.completed",
					"toolCallId": rec.ToolCallID,
					"name":       rec.Inv.Name,
					"status":     rec.Inv.Status,
					"durationMs": rec.Inv.DurationMs,
				})
			},
		}
	}

	var turn *chatTurnResult
	if stream != nil {
		turn, err = s.chat.runChatCompletionStreaming(ctx, msgsForLLM, prevResponseID, hooks)
	} else {
		turn, err = s.chat.runChatCompletionWithHistory(ctx, msgsForLLM, prevResponseID)
	}

	// If OpenAI rejected our previous_response_id (response aged out, key rotated
	// across projects, etc.), recover transparently: drop the stale id, send the
	// full compacted history on a fresh thread, and retry once. Any further error
	// is surfaced to the client.
	if err != nil && prevResponseID != nil && IsInvalidPrevResponseIDError(err) {
		log.WithFields(log.Fields{
			"chatId":        chat.ID,
			"userId":        userID,
			"correlationId": AiChatCorrelationIDFromContext(ctx),
		}).Warn("ai-chat: invalid previous_response_id on OpenAI side; resetting and retrying with full history")
		chat.OpenAIPreviousResponseID = nil
		fallbackMsgs := s.buildHistoryForLLM(chat, hist)
		if stream != nil {
			turn, err = s.chat.runChatCompletionStreaming(ctx, fallbackMsgs, nil, hooks)
		} else {
			turn, err = s.chat.runChatCompletionWithHistory(ctx, fallbackMsgs, nil)
		}
	}

	if err != nil {
		if stream != nil {
			_ = s.emitStream(stream, "error", map[string]interface{}{
				"type":    "error",
				"code":    "APIHUB-AI-5001",
				"message": err.Error(),
			})
		}
		return nil, nil, err
	}

	createdA := time.Now().UTC()
	var compPtr *string
	if turn.OpenAICompletionID != "" {
		c := turn.OpenAICompletionID
		compPtr = &c
	}
	assistantEnt := &entity.AiChatMessageEntity{
		ID:               assistantID,
		ChatID:           chat.ID,
		Role:             "assistant",
		Content:          turn.AssistantContent,
		ToolInvocations:  turn.ToolInvocations,
		OpenaiResponseID: compPtr,
		CreatedAt:        createdA,
	}
	if err := s.repo.InsertMessage(ctx, assistantEnt); err != nil {
		return nil, nil, err
	}
	chat.LastMessageAt = createdA
	wasFirstAssistantTurn := chat.MessagesCount == 0 && strings.TrimSpace(chat.Title) == ""
	chat.MessagesCount += 2
	chat.OpenAIPreviousResponseID = compPtr
	if turn.Usage != nil {
		tk := int(turn.Usage.TotalTokens)
		chat.LastTurnTokens = &tk
		metrics.AiChatTurnTokens.WithLabelValues(streamModeForChan(stream)).Observe(float64(turn.Usage.TotalTokens))
	}
	for _, inv := range turn.ToolInvocations {
		metrics.AiChatToolCallsTotal.WithLabelValues(inv.Name, inv.Status).Inc()
	}
	if err := s.repo.UpdateChat(ctx, chat); err != nil {
		return nil, nil, err
	}

	if wasFirstAssistantTurn {
		s.scheduleAutoTitle(chat.ID, chat.UserID, um.Content, turn.AssistantContent)
	}

	umView, _ := s.entityToMessageView(ctx, userID, um)
	amView, _ := s.entityToMessageView(ctx, userID, assistantEnt)

	if stream != nil {
		_ = s.emitStream(stream, "message.assistant.completed", map[string]interface{}{
			"type": "message.assistant.completed", "message": amView,
		})
		_ = s.emitStream(stream, "done", map[string]interface{}{"type": "done"})
	}

	return umView, amView, nil
}

// replayIdempotent is invoked when InsertMessage hits the partial unique index on
// (chat_id, client_message_id). It loads the existing user message and either replays the
// cached assistant response (if present) or retries the LLM call (if the previous attempt
// died before persisting the assistant message).
func (s *aiChatServiceImpl) replayIdempotent(ctx context.Context, userID, chatID, clientID string, stream chan<- AiChatStreamChunk) (*view.AiChatMessage, *view.AiChatMessage, error) {
	u, err := s.repo.FindUserMessageByClientID(ctx, chatID, clientID)
	if err != nil || u == nil {
		return nil, nil, &exception.CustomError{Status: 500, Code: "500", Message: "Idempotent replay failed"}
	}
	a, err := s.repo.FindNextAssistantMessage(ctx, chatID, u.CreatedAt)
	if err != nil {
		return nil, nil, err
	}
	if a == nil {
		chat, err := s.repo.GetChatByIDForUser(ctx, chatID, userID)
		if err != nil || chat == nil {
			return nil, nil, &exception.CustomError{Status: 500, Code: "500", Message: "Idempotent retry failed"}
		}
		return s.runLLMTurn(ctx, userID, chat, u, stream)
	}
	return s.serveCachedPair(ctx, userID, u, a, stream)
}

// serveCachedPair returns an already-stored (user, assistant) pair and, when streaming, replays
// the assistant message as message.assistant.* events so the client sees the same shape as a
// fresh turn.
func (s *aiChatServiceImpl) serveCachedPair(ctx context.Context, userID string, u, a *entity.AiChatMessageEntity, stream chan<- AiChatStreamChunk) (*view.AiChatMessage, *view.AiChatMessage, error) {
	umView, _ := s.entityToMessageView(ctx, userID, u)
	amView, _ := s.entityToMessageView(ctx, userID, a)
	if stream != nil {
		_ = s.emitStream(stream, "message.assistant.start", map[string]interface{}{
			"type": "message.assistant.start", "messageId": a.ID,
		})
		if amView != nil {
			runes := []rune(amView.Content)
			for i := 0; i < len(runes); i += 64 {
				end := i + 64
				if end > len(runes) {
					end = len(runes)
				}
				_ = s.emitStream(stream, "message.assistant.delta", map[string]interface{}{
					"type": "message.assistant.delta", "delta": string(runes[i:end]),
				})
			}
		}
		_ = s.emitStream(stream, "message.assistant.completed", map[string]interface{}{
			"type": "message.assistant.completed", "message": amView,
		})
		_ = s.emitStream(stream, "done", map[string]interface{}{"type": "done"})
	}
	return umView, amView, nil
}

func (s *aiChatServiceImpl) historyToViewChat(rows []entity.AiChatMessageEntity) []ChatMessage {
	out := make([]ChatMessage, 0, len(rows))
	for i := range rows {
		if rows[i].Role != "user" && rows[i].Role != "assistant" {
			continue
		}
		out = append(out, ChatMessage{Role: rows[i].Role, Content: rows[i].Content})
	}
	return out
}

func (s *aiChatServiceImpl) entityToMessageView(ctx context.Context, userID string, m *entity.AiChatMessageEntity) (*view.AiChatMessage, error) {
	content := m.Content
	if m.Role == "assistant" {
		var err error
		content, err = s.resignGeneratedFileLinksInContent(ctx, userID, m.Content)
		if err != nil {
			return nil, err
		}
	}
	var cl *string
	if m.ClientMessageID != nil {
		c := *m.ClientMessageID
		cl = &c
	}
	return &view.AiChatMessage{
		MessageID:        m.ID,
		ClientMessageID:  cl,
		Role:             m.Role,
		Content:          content,
		CreatedAt:        m.CreatedAt.UTC().Format(time.RFC3339),
		ToolInvocations:  m.ToolInvocations,
	}, nil
}

// scheduleAutoTitle generates and persists a short title in the background
// after the very first assistant turn, but only if the user has not provided one.
func (s *aiChatServiceImpl) scheduleAutoTitle(chatID, userID, userText, assistantText string) {
	utils.SafeAsync(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		title := s.chat.generateChatTitle(ctx, userText, assistantText)
		if strings.TrimSpace(title) == "" {
			return
		}
		current, err := s.repo.GetChatByIDForUser(ctx, chatID, userID)
		if err != nil || current == nil {
			return
		}
		if strings.TrimSpace(current.Title) != "" {
			return
		}
		current.Title = title
		if err := s.repo.UpdateChat(ctx, current); err != nil {
			// title generation is best-effort; just log
			fmt.Printf("[ai-chat] auto-title persist failed for chat %s: %v\n", chatID, err)
		}
	})
}

// maybeCompactBefore checks whether the previous turn pushed token usage close to the
// model's context window. If yes, it summarizes the older slice of messages, persists the
// summary, and returns (true, sse-event-payload). The compaction boundary is chosen as
// "all messages except the last minRecentMessagesAfterCompaction".
func (s *aiChatServiceImpl) maybeCompactBefore(ctx context.Context, chat *entity.AiChatEntity, hist []entity.AiChatMessageEntity) (bool, map[string]interface{}) {
	if chat.LastTurnTokens == nil {
		return false, nil
	}
	cfg := s.sis.GetAiChatConfig()
	pct := cfg.CompactAtContextPercent
	if pct <= 0 || pct >= 100 {
		pct = 80
	}
	ctxWindow := s.chat.ModelContextWindow()
	threshold := ctxWindow * pct / 100
	if *chat.LastTurnTokens < threshold {
		return false, nil
	}

	// Choose boundary: keep last N verbatim, compress the older head.
	keep := minRecentMessagesAfterCompaction
	if len(hist) <= keep {
		return false, nil
	}
	headEnd := len(hist) - keep
	headSlice := hist[:headEnd]

	// If we already compacted up to (or past) the head's end, nothing new to summarize.
	if chat.CompactedUpToCreatedAt != nil &&
		!headSlice[len(headSlice)-1].CreatedAt.After(*chat.CompactedUpToCreatedAt) {
		return false, nil
	}

	headView := s.historyToViewChat(headSlice)
	summary := s.chat.summarizeMessagesForCompaction(ctx, chat.CompactionSummary, headView)
	if strings.TrimSpace(summary) == "" {
		return false, nil
	}
	boundary := headSlice[len(headSlice)-1].CreatedAt
	chat.CompactionSummary = &summary
	chat.CompactedUpToCreatedAt = &boundary
	// Reset previous_response_id: server-side LLM context tracked via that id
	// no longer matches our compacted view.
	chat.OpenAIPreviousResponseID = nil
	chat.LastTurnTokens = nil
	if err := s.repo.UpdateChat(ctx, chat); err != nil {
		// Best-effort: a failed UPDATE just means no compaction takes effect this turn.
		fmt.Printf("[ai-chat] persist compaction failed for chat %s: %v\n", chat.ID, err)
		return false, nil
	}
	return true, map[string]interface{}{
		"type":            "context.compacted",
		"compactedUpTo":   boundary.UTC().Format(time.RFC3339),
		"summaryPreview":  truncateRunes(summary, 240),
		"messagesBefore":  len(hist),
		"messagesKeptRaw": keep,
	}
}

// buildHistoryForLLM produces the ChatMessage slice fed to the LLM, prepending the compaction
// summary (as an extra system-style block) and dropping any messages older than the boundary.
func (s *aiChatServiceImpl) buildHistoryForLLM(chat *entity.AiChatEntity, hist []entity.AiChatMessageEntity) []ChatMessage {
	rows := hist
	if chat.CompactedUpToCreatedAt != nil {
		filtered := rows[:0:0]
		for i := range rows {
			if rows[i].CreatedAt.After(*chat.CompactedUpToCreatedAt) {
				filtered = append(filtered, rows[i])
			}
		}
		rows = filtered
	}
	out := make([]ChatMessage, 0, len(rows)+1)
	if chat.CompactionSummary != nil && strings.TrimSpace(*chat.CompactionSummary) != "" {
		out = append(out, ChatMessage{
			Role: "system",
			Content: "EARLIER CONVERSATION SUMMARY (replaces older messages, keep using these facts):\n" +
				*chat.CompactionSummary,
		})
	}
	out = append(out, s.historyToViewChat(rows)...)
	return out
}

func entityToAiChatView(e *entity.AiChatEntity) *view.AiChat {
	pinned := e.Pinned
	var pp *bool
	if pinned {
		pp = &pinned
	}
	return &view.AiChat{
		ChatID:        e.ID,
		Title:         e.Title,
		Pinned:        pp,
		CreatedAt:     e.CreatedAt.UTC().Format(time.RFC3339),
		LastMessageAt: e.LastMessageAt.UTC().Format(time.RFC3339),
		MessagesCount: e.MessagesCount,
	}
}
