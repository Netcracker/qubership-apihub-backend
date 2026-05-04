package repository

import (
	"context"
	"strings"
	"time"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/db"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/entity"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/utils"
	"github.com/go-pg/pg/v10"
)

func NewAiChatRepositoryPG(cp db.ConnectionProvider) (AiChatRepository, error) {
	return &aiChatRepositoryImpl{cp: cp}, nil
}

type aiChatRepositoryImpl struct {
	cp db.ConnectionProvider
}

func (r *aiChatRepositoryImpl) CreateChat(ctx context.Context, row *entity.AiChatEntity) error {
	_, err := r.cp.GetConnection().ModelContext(ctx, row).Insert()
	return err
}

func (r *aiChatRepositoryImpl) GetChatByIDForUser(ctx context.Context, chatID, userID string) (*entity.AiChatEntity, error) {
	res := new(entity.AiChatEntity)
	err := r.cp.GetConnection().ModelContext(ctx, res).
		Where("id = ?", chatID).
		Where("user_id = ?", userID).
		Limit(1).
		Select()
	if err != nil {
		if err == pg.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return res, nil
}

func (r *aiChatRepositoryImpl) UpdateChat(ctx context.Context, row *entity.AiChatEntity) error {
	// Raw SQL avoids go-pg's zerochecker, which silently maps empty string / false / 0
	// to NULL when using the ORM UPDATE helpers (even with explicit .Column()).
	// With direct parameter binding:
	//   - string ""  → '' (empty string, satisfies NOT NULL)
	//   - bool false → false (needed for unpin operations)
	//   - *T nil     → NULL  (for nullable columns)
	_, err := r.cp.GetConnection().ExecContext(ctx, `
		UPDATE ai_chat
		SET title                       = ?,
		    pinned                      = ?,
		    last_message_at             = ?,
		    messages_count              = ?,
		    openai_previous_response_id = ?,
		    compacted_up_to_created_at  = ?,
		    compaction_summary          = ?,
		    last_turn_tokens            = ?
		WHERE id = ?`,
		row.Title,
		row.Pinned,
		row.LastMessageAt,
		row.MessagesCount,
		row.OpenAIPreviousResponseID,
		row.CompactedUpToCreatedAt,
		row.CompactionSummary,
		row.LastTurnTokens,
		row.ID,
	)
	return err
}

func (r *aiChatRepositoryImpl) DeleteChat(ctx context.Context, chatID, userID string) (int, error) {
	res, err := r.cp.GetConnection().ModelContext(ctx, (*entity.AiChatEntity)(nil)).
		TableExpr("ai_chat").
		Where("id = ?", chatID).
		Where("user_id = ?", userID).
		Delete()
	if err != nil {
		return 0, err
	}
	return res.RowsAffected(), nil
}

func (r *aiChatRepositoryImpl) ListChats(ctx context.Context, f AiChatsListFilter) ([]entity.AiChatEntity, error) {
	var rows []entity.AiChatEntity
	q := r.cp.GetConnection().ModelContext(ctx, &rows).
		Where("user_id = ?", f.UserID)

	if strings.TrimSpace(f.Search) != "" {
		pat := "%" + utils.LikeEscaped(f.Search) + "%"
		q = q.Where("title ILIKE ?", pat)
	}
	if f.Before != nil {
		q = q.Where("last_message_at < ?", f.Before.UTC())
	}
	q = q.OrderExpr("pinned DESC, last_message_at DESC, id DESC")
	if f.Limit > 0 {
		q = q.Limit(f.Limit)
	}
	err := q.Select()
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *aiChatRepositoryImpl) CountPinnedChats(ctx context.Context, userID string) (int, error) {
	return r.cp.GetConnection().ModelContext(ctx, (*entity.AiChatEntity)(nil)).
		TableExpr("ai_chat").
		Where("user_id = ? AND pinned = true", userID).
		Count()
}

func (r *aiChatRepositoryImpl) InsertMessage(ctx context.Context, m *entity.AiChatMessageEntity) error {
	_, err := r.cp.GetConnection().ModelContext(ctx, m).Insert()
	return err
}

func (r *aiChatRepositoryImpl) TryInsertUserMessageIdempotent(ctx context.Context, m *entity.AiChatMessageEntity) (bool, error) {
	if m.ClientMessageID != nil {
		existing, err := r.FindUserMessageByClientID(ctx, m.ChatID, *m.ClientMessageID)
		if err != nil {
			return false, err
		}
		if existing != nil {
			return false, nil
		}
	}
	if err := r.InsertMessage(ctx, m); err != nil {
		if m.ClientMessageID != nil && isUniqueViolation(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	if pgErr, ok := err.(pg.Error); ok {
		return pgErr.IntegrityViolation()
	}
	return strings.Contains(err.Error(), "23505")
}

func (r *aiChatRepositoryImpl) FindUserMessageByClientID(ctx context.Context, chatID, clientMsgID string) (*entity.AiChatMessageEntity, error) {
	res := new(entity.AiChatMessageEntity)
	err := r.cp.GetConnection().ModelContext(ctx, res).
		Where("chat_id = ?", chatID).
		Where("client_message_id = ?", clientMsgID).
		Limit(1).
		Select()
	if err != nil {
		if err == pg.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return res, nil
}

func (r *aiChatRepositoryImpl) FindNextAssistantMessage(ctx context.Context, chatID string, afterCreatedAt time.Time) (*entity.AiChatMessageEntity, error) {
	res := new(entity.AiChatMessageEntity)
	err := r.cp.GetConnection().ModelContext(ctx, res).
		Where("chat_id = ?", chatID).
		Where("role = ?", "assistant").
		Where("created_at > ?", afterCreatedAt.UTC()).
		OrderExpr("created_at ASC").
		Limit(1).
		Select()
	if err != nil {
		if err == pg.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return res, nil
}

func (r *aiChatRepositoryImpl) ListMessages(ctx context.Context, f AiMessagesListFilter) ([]entity.AiChatMessageEntity, error) {
	var rows []entity.AiChatMessageEntity
	q := r.cp.GetConnection().ModelContext(ctx, &rows).
		Where("chat_id = ?", f.ChatID)
	if f.Before != nil {
		q = q.Where("created_at < ?", f.Before.UTC())
	}
	q = q.OrderExpr("created_at DESC")
	if f.Limit > 0 {
		q = q.Limit(f.Limit)
	}
	err := q.Select()
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *aiChatRepositoryImpl) ListMessagesChronological(ctx context.Context, chatID string, maxMessages int) ([]entity.AiChatMessageEntity, error) {
	var rows []entity.AiChatMessageEntity
	if maxMessages < 1 {
		maxMessages = 200
	}
	err := r.cp.GetConnection().ModelContext(ctx, &rows).
		Where("chat_id = ?", chatID).
		OrderExpr("created_at ASC").
		Limit(maxMessages).
		Select()
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *aiChatRepositoryImpl) GetFileByIDForUser(ctx context.Context, fileID, userID string) (*entity.AiChatFileEntity, error) {
	res := new(entity.AiChatFileEntity)
	err := r.cp.GetConnection().ModelContext(ctx, res).
		Where("id = ?", fileID).
		Where("user_id = ?", userID).
		Limit(1).
		Select()
	if err != nil {
		if err == pg.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return res, nil
}

func (r *aiChatRepositoryImpl) InsertFile(ctx context.Context, f *entity.AiChatFileEntity) error {
	_, err := r.cp.GetConnection().ModelContext(ctx, f).Insert()
	return err
}

// ListUserIDs returns distinct user ids that have any chat row
func (r *aiChatRepositoryImpl) ListUserIDs(ctx context.Context) ([]string, error) {
	var ids []string
	_, err := r.cp.GetConnection().QueryContext(ctx, &ids, "SELECT DISTINCT user_id FROM ai_chat")
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// DeleteUserChatsByRetention removes non-pinned chats older than retentionDays for a user,
// keeping the most recent pinnedForeverCount non-pinned ones; pinned chats are never removed.
func (r *aiChatRepositoryImpl) DeleteUserChatsByRetention(ctx context.Context, userID string, retentionDays, pinnedForeverCount int) (int, error) {
	if retentionDays < 1 {
		return 0, nil
	}
	if pinnedForeverCount < 0 {
		pinnedForeverCount = 0
	}
	q := `
DELETE FROM ai_chat c
WHERE c.user_id = ?
	AND c.pinned = false
	AND c.last_message_at < (now() at time zone 'utc') - make_interval(days => ?)
	AND c.id NOT IN (
			SELECT id FROM ai_chat
			WHERE user_id = ? AND pinned = false
			ORDER BY last_message_at DESC
			LIMIT ?
	)`
	res, err := r.cp.GetConnection().ExecContext(ctx, q, userID, retentionDays, userID, pinnedForeverCount)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected(), nil
}

func (r *aiChatRepositoryImpl) ListExpiredFiles(ctx context.Context, limit int) ([]entity.AiChatFileEntity, error) {
	if limit < 1 {
		limit = 1000
	}
	var rows []entity.AiChatFileEntity
	err := r.cp.GetConnection().ModelContext(ctx, &rows).
		Where("expires_at < ?", time.Now().UTC()).
		Limit(limit).
		Select()
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *aiChatRepositoryImpl) DeleteFileByID(ctx context.Context, fileID string) error {
	_, err := r.cp.GetConnection().ModelContext(ctx, (*entity.AiChatFileEntity)(nil)).
		TableExpr("ai_chat_file").
		Where("id = ?", fileID).
		Delete()
	return err
}
