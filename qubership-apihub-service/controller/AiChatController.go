package controller

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/context"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/exception"
	aiservice "github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/service"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/utils"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
)

// AiChatController productized /api/v1/ai-chat/chats
type AiChatController struct {
	svc aiservice.AiChatService
}

func NewAiChatController(svc aiservice.AiChatService) *AiChatController {
	return &AiChatController{svc: svc}
}

func (c *AiChatController) ListChats(w http.ResponseWriter, r *http.Request) {
	uid := context.Create(r).GetUserId()
	limit, ce := getLimitQueryParam(r)
	if ce != nil {
		utils.RespondWithCustomError(w, ce)
		return
	}
	var before *time.Time
	if b := r.URL.Query().Get("before"); b != "" {
		t, err := time.Parse(time.RFC3339, b)
		if err != nil {
			utils.RespondWithCustomError(w, &exception.CustomError{Status: http.StatusBadRequest, Code: exception.InvalidParameterValue, Message: "Invalid before cursor", Debug: err.Error()})
			return
		}
		before = &t
	}
	search := r.URL.Query().Get("search")
	res, err := c.svc.ListChats(r.Context(), uid, search, before, limit)
	if err != nil {
		utils.RespondWithError(w, "list chats", err)
		return
	}
	utils.RespondWithJson(w, http.StatusOK, res)
}

func (c *AiChatController) CreateChat(w http.ResponseWriter, r *http.Request) {
	uid := context.Create(r).GetUserId()
	var body view.AiChatCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err != io.EOF {
		utils.RespondWithCustomError(w, &exception.CustomError{Status: http.StatusBadRequest, Code: exception.BadRequestBody, Message: exception.BadRequestBodyMsg, Debug: err.Error()})
		return
	}
	res, err := c.svc.CreateChat(r.Context(), uid, body.Title)
	if err != nil {
		utils.RespondWithError(w, "create chat", err)
		return
	}
	utils.RespondWithJson(w, http.StatusCreated, res)
}

func (c *AiChatController) GetChat(w http.ResponseWriter, r *http.Request) {
	uid := context.Create(r).GetUserId()
	chatID := getStringParam(r, "chatId")
	res, err := c.svc.GetChat(r.Context(), uid, chatID)
	if err != nil {
		utils.RespondWithError(w, "get chat", err)
		return
	}
	utils.RespondWithJson(w, http.StatusOK, res)
}

func (c *AiChatController) UpdateChat(w http.ResponseWriter, r *http.Request) {
	uid := context.Create(r).GetUserId()
	chatID := getStringParam(r, "chatId")
	var body view.AiChatUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		utils.RespondWithCustomError(w, &exception.CustomError{Status: http.StatusBadRequest, Code: exception.BadRequestBody, Message: exception.BadRequestBodyMsg, Debug: err.Error()})
		return
	}
	res, err := c.svc.UpdateChat(r.Context(), uid, chatID, &body)
	if err != nil {
		utils.RespondWithError(w, "update chat", err)
		return
	}
	utils.RespondWithJson(w, http.StatusOK, res)
}

func (c *AiChatController) DeleteChat(w http.ResponseWriter, r *http.Request) {
	uid := context.Create(r).GetUserId()
	chatID := getStringParam(r, "chatId")
	if err := c.svc.DeleteChat(r.Context(), uid, chatID); err != nil {
		utils.RespondWithError(w, "delete chat", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (c *AiChatController) ListMessages(w http.ResponseWriter, r *http.Request) {
	uid := context.Create(r).GetUserId()
	chatID := getStringParam(r, "chatId")
	limit, ce := getLimitQueryParam(r)
	if ce != nil {
		utils.RespondWithCustomError(w, ce)
		return
	}
	var before *time.Time
	if b := r.URL.Query().Get("before"); b != "" {
		t, err := time.Parse(time.RFC3339, b)
		if err != nil {
			utils.RespondWithCustomError(w, &exception.CustomError{Status: http.StatusBadRequest, Code: exception.InvalidParameterValue, Message: "Invalid before", Debug: err.Error()})
			return
		}
		before = &t
	}
	res, err := c.svc.ListMessages(r.Context(), uid, chatID, before, limit)
	if err != nil {
		utils.RespondWithError(w, "list messages", err)
		return
	}
	utils.RespondWithJson(w, http.StatusOK, res)
}

func (c *AiChatController) SendMessage(w http.ResponseWriter, r *http.Request) {
	uid := context.Create(r).GetUserId()
	chatID := getStringParam(r, "chatId")
	var body view.AiChatSendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		utils.RespondWithCustomError(w, &exception.CustomError{Status: http.StatusBadRequest, Code: exception.BadRequestBody, Message: exception.BadRequestBodyMsg, Debug: err.Error()})
		return
	}
	if err := utils.ValidateObject(body); err != nil {
		utils.RespondWithError(w, "validate send message request", err)
		return
	}
	res, err := c.svc.SendMessage(r.Context(), uid, chatID, &body)
	if err != nil {
		utils.RespondWithError(w, "send", err)
		return
	}
	utils.RespondWithJson(w, http.StatusOK, res)
}

func (c *AiChatController) SendMessageStream(w http.ResponseWriter, r *http.Request) {
	uid := context.Create(r).GetUserId()
	chatID := getStringParam(r, "chatId")
	var body view.AiChatSendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		utils.RespondWithCustomError(w, &exception.CustomError{Status: http.StatusBadRequest, Code: exception.BadRequestBody, Message: exception.BadRequestBodyMsg, Debug: err.Error()})
		return
	}
	if err := utils.ValidateObject(body); err != nil {
		utils.RespondWithError(w, "validate stream request", err)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	fl, ok := w.(http.Flusher)
	if !ok {
		utils.RespondWithCustomError(w, &exception.CustomError{Status: http.StatusInternalServerError, Code: exception.AiChatInternalError, Message: "Streaming not supported"})
		return
	}

	ch, err := c.svc.SendMessageStream(r.Context(), uid, chatID, &body)
	if err != nil {
		utils.RespondWithError(w, "stream", err)
		return
	}

	for item := range ch {
		b, _ := json.Marshal(item.Data)
		_, _ = io.WriteString(w, "event: "+item.EventName+"\n")
		_, _ = io.WriteString(w, "data: "+string(b)+"\n\n")
		fl.Flush()
	}
}
