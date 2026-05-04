package entity

import "time"

// AiChatFileEntity maps to public.ai_chat_file
type AiChatFileEntity struct {
	tableName struct{} `pg:"ai_chat_file, alias:ai_chat_file"`

	ID          string     `pg:"id, pk, type:uuid"`
	ChatID      *string    `pg:"chat_id, type:uuid"`
	MessageID   *string    `pg:"message_id, type:uuid"`
	UserID      string     `pg:"user_id, type:varchar"`
	Filename    string     `pg:"filename, type:text"`
	StoragePath string     `pg:"storage_path, type:text"`
	MimeType    *string    `pg:"mime_type, type:varchar"`
	SizeBytes   *int64     `pg:"size_bytes"`
	CreatedAt   time.Time  `pg:"created_at"`
	ExpiresAt   time.Time  `pg:"expires_at"`
}
