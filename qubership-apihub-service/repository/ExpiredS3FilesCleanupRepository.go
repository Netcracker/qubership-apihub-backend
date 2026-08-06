package repository

import (
	"context"
	"time"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/db"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/entity"
)

type ExpiredS3FilesCleanupRepository interface {
	StoreCleanupRun(ctx context.Context, ent entity.ExpiredS3FilesCleanupEntity) error
	UpdateCleanupRun(ctx context.Context, runId string, details string, deletedItems int, finishedAt *time.Time) error
}

func NewExpiredS3FilesCleanupRepository(cp db.ConnectionProvider) ExpiredS3FilesCleanupRepository {
	return &expiredS3FilesCleanupRepositoryImpl{cp: cp}
}

type expiredS3FilesCleanupRepositoryImpl struct {
	cp db.ConnectionProvider
}

func (s expiredS3FilesCleanupRepositoryImpl) StoreCleanupRun(ctx context.Context, ent entity.ExpiredS3FilesCleanupEntity) error {
	_, err := s.cp.GetConnection().ModelContext(ctx, &ent).Insert()
	return err
}

func (s expiredS3FilesCleanupRepositoryImpl) UpdateCleanupRun(ctx context.Context, runId string, details string, deletedItems int, finishedAt *time.Time) error {
	query := s.cp.GetConnection().ModelContext(ctx, &entity.ExpiredS3FilesCleanupEntity{}).Set("deleted_items=?", deletedItems)

	if details != "" {
		query = query.Set("details=?", details)
	}

	if finishedAt != nil {
		query = query.Set("finished_at=?", finishedAt)
	}

	_, err := query.Where("run_id=?", runId).Update()
	return err
}
