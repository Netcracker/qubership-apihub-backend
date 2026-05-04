package entity

import (
	"time"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
)

// AiChatMessageEntity maps to public.ai_chat_message
type AiChatMessageEntity struct {
	tableName struct{} `pg:"ai_chat_message, alias:ai_chat_message"`

	ID               string                       `pg:"id, pk, type:uuid"`
	ChatID           string                       `pg:"chat_id, type:uuid"`
	Role             string                       `pg:"role, type:varchar"`
	Content          string                       `pg:"content, type:text"`
	ClientMessageID  *string                      `pg:"client_message_id, type:uuid"`
	ToolInvocations  []view.AiChatToolInvocation  `pg:"tool_invocations, type:jsonb"`
	OpenaiResponseID *string                      `pg:"openai_response_id, type:text"`
	CreatedAt        time.Time                    `pg:"created_at"`
}
