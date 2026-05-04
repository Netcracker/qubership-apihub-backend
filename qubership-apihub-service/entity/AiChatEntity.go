package entity

import (
	"time"
)

// AiChatEntity maps to public.ai_chat
type AiChatEntity struct {
	tableName struct{} `pg:"ai_chat, alias:ai_chat"`

	ID                         string    `pg:"id, pk, type:uuid"`
	UserID                     string    `pg:"user_id, type:varchar"`
	Title                      string    `pg:"title, type:text"`
	Pinned                     bool      `pg:"pinned"`
	CreatedAt                  time.Time `pg:"created_at"`
	LastMessageAt              time.Time `pg:"last_message_at"`
	MessagesCount              int       `pg:"messages_count"`
	OpenAIPreviousResponseID   *string   `pg:"openai_previous_response_id"`
	CompactedUpToCreatedAt     *time.Time `pg:"compacted_up_to_created_at"`
	CompactionSummary          *string   `pg:"compaction_summary, type:text"`
	LastTurnTokens             *int     `pg:"last_turn_tokens"`
}
