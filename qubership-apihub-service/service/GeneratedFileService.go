package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/entity"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/metrics"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/repository"
	"github.com/google/uuid"
)

// GeneratedFileService persists LLM-produced files in a temp directory and writes a row to ai_chat_file.
// Files are referenced by inline Markdown links of the form
//   [<filename>](<apihubUrl>/api/v1/generated-files/<fileId>?token=<jwt>)
// Tokens are minted by the caller (typically AiChatService) when reading messages.
type GeneratedFileService interface {
	// SaveFile streams content into <baseDir>/<userId>/<fileId>, writes a DB row, and returns
	// the resulting entity together with a Markdown-ready URL (without ?token=, see AiChatService).
	SaveFile(ctx context.Context, in GeneratedFileSaveInput) (*entity.AiChatFileEntity, string, error)
}

// GeneratedFileSaveInput is the descriptor accepted by SaveFile.
type GeneratedFileSaveInput struct {
	UserID    string
	ChatID    *string
	MessageID *string
	Filename  string
	MimeType  string
	Reader    io.Reader
	// MaxBytes optionally caps streamed bytes; if 0 the service will use config (MaxFileSizeMB).
	MaxBytes int64
}

func NewGeneratedFileService(sis SystemInfoService, repo repository.AiChatRepository) GeneratedFileService {
	return &generatedFileServiceImpl{sis: sis, repo: repo}
}

type generatedFileServiceImpl struct {
	sis  SystemInfoService
	repo repository.AiChatRepository
}

func (s *generatedFileServiceImpl) SaveFile(ctx context.Context, in GeneratedFileSaveInput) (*entity.AiChatFileEntity, string, error) {
	if in.Reader == nil {
		return nil, "", errors.New("nil reader")
	}
	if strings.TrimSpace(in.UserID) == "" {
		return nil, "", errors.New("userID is required")
	}
	cfg := s.sis.GetAiChatConfig().GeneratedFiles
	baseDir := strings.TrimSpace(cfg.Directory)
	if baseDir == "" {
		baseDir = filepath.Join(os.TempDir(), "apihub-ai-chat-files")
	}
	maxBytes := in.MaxBytes
	if maxBytes <= 0 {
		if cfg.MaxFileSizeMB > 0 {
			maxBytes = int64(cfg.MaxFileSizeMB) * 1024 * 1024
		} else {
			maxBytes = 50 * 1024 * 1024
		}
	}
	ttl := time.Duration(cfg.TTLMinutes) * time.Minute
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}

	id := uuid.NewString()
	userDir := filepath.Join(baseDir, sanitizeUserID(in.UserID))
	if err := os.MkdirAll(userDir, 0o700); err != nil {
		return nil, "", fmt.Errorf("create user dir: %w", err)
	}
	storagePath := filepath.Join(userDir, id)

	written, err := streamToFile(storagePath, in.Reader, maxBytes)
	if err != nil {
		_ = os.Remove(storagePath)
		return nil, "", err
	}

	now := time.Now().UTC()
	mime := strings.TrimSpace(in.MimeType)
	var mimePtr *string
	if mime != "" {
		mimePtr = &mime
	}
	size := written
	row := &entity.AiChatFileEntity{
		ID:          id,
		ChatID:      in.ChatID,
		MessageID:   in.MessageID,
		UserID:      in.UserID,
		Filename:    sanitizeFilename(in.Filename, id),
		StoragePath: storagePath,
		MimeType:    mimePtr,
		SizeBytes:   &size,
		CreatedAt:   now,
		ExpiresAt:   now.Add(ttl),
	}
	if err := s.repo.InsertFile(ctx, row); err != nil {
		_ = os.Remove(storagePath)
		return nil, "", fmt.Errorf("insert file row: %w", err)
	}
	metrics.AiChatGeneratedFilesTotal.Inc()
	metrics.AiChatGeneratedFileBytes.Observe(float64(written))

	return row, "/api/v1/generated-files/" + id, nil
}

func streamToFile(path string, r io.Reader, maxBytes int64) (int64, error) {
	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, fmt.Errorf("open output file: %w", err)
	}
	defer out.Close()
	limited := io.LimitReader(r, maxBytes+1)
	written, err := io.Copy(out, limited)
	if err != nil {
		return written, fmt.Errorf("write file: %w", err)
	}
	if written > maxBytes {
		return written, fmt.Errorf("file exceeds maximum size of %d bytes", maxBytes)
	}
	return written, nil
}

// sanitizeUserID removes path separators and url-decodes anything fancy so we keep the FS layout flat.
func sanitizeUserID(uid string) string {
	uid = url.PathEscape(uid)
	uid = strings.ReplaceAll(uid, string(os.PathSeparator), "_")
	if uid == "" {
		uid = "anon"
	}
	if len(uid) > 64 {
		uid = uid[:64]
	}
	return uid
}

func sanitizeFilename(name, fallback string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return fallback
	}
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\x00", "")
	if len(name) > 255 {
		name = name[:255]
	}
	return name
}
