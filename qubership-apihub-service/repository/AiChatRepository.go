package repository

import (
	"context"
	"time"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/entity"
)

// AiChatsListFilter is keyset + search for listing chats
type AiChatsListFilter struct {
	UserID  string
	Search  string
	Before  *time.Time
	Limit   int
}

// AiMessagesListFilter lists messages in reverse chrono
type AiMessagesListFilter struct {
	ChatID string
	Before *time.Time
	Limit  int
}

// AiChatRepository is persistence for productized AI chat
type AiChatRepository interface {
	CreateChat(ctx context.Context, row *entity.AiChatEntity) error
	GetChatByIDForUser(ctx context.Context, chatID, userID string) (*entity.AiChatEntity, error)
	UpdateChat(ctx context.Context, row *entity.AiChatEntity) error
	DeleteChat(ctx context.Context, chatID, userID string) (int, error)
	ListChats(ctx context.Context, f AiChatsListFilter) ([]entity.AiChatEntity, error)
	CountPinnedChats(ctx context.Context, userID string) (int, error)

	InsertMessage(ctx context.Context, m *entity.AiChatMessageEntity) error
	TryInsertUserMessageIdempotent(ctx context.Context, m *entity.AiChatMessageEntity) (inserted bool, err error)
	FindUserMessageByClientID(ctx context.Context, chatID, clientMsgID string) (*entity.AiChatMessageEntity, error)
	FindNextAssistantMessage(ctx context.Context, chatID string, afterCreatedAt time.Time) (*entity.AiChatMessageEntity, error)
	ListMessages(ctx context.Context, f AiMessagesListFilter) ([]entity.AiChatMessageEntity, error)
	// ListMessagesChronological returns messages oldest-first (for model context)
	ListMessagesChronological(ctx context.Context, chatID string, maxMessages int) ([]entity.AiChatMessageEntity, error)
	GetFileByIDForUser(ctx context.Context, fileID, userID string) (*entity.AiChatFileEntity, error)
	InsertFile(ctx context.Context, f *entity.AiChatFileEntity) error

	// Cleanup helpers
	ListUserIDs(ctx context.Context) ([]string, error)
	DeleteUserChatsByRetention(ctx context.Context, userID string, retentionDays, pinnedForeverCount int) (int, error)
	ListExpiredFiles(ctx context.Context, limit int) ([]entity.AiChatFileEntity, error)
	DeleteFileByID(ctx context.Context, fileID string) error
}
