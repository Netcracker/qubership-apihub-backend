package cleanup

import (
	"context"
	"fmt"
	"time"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/entity"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/repository"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/service"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/service/cleanup/logger"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
)

type expiredS3FilesCleanupJobProcessor struct {
	minioStorageService       service.MinioStorageService
	expiredS3FilesCleanupRepo repository.ExpiredS3FilesCleanupRepository
}

func NewExpiredS3FilesCleanupJobProcessor(
	minioStorageService service.MinioStorageService,
	expiredS3FilesCleanupRepo repository.ExpiredS3FilesCleanupRepository,
) JobProcessor {
	return &expiredS3FilesCleanupJobProcessor{
		minioStorageService:       minioStorageService,
		expiredS3FilesCleanupRepo: expiredS3FilesCleanupRepo,
	}
}

func (p *expiredS3FilesCleanupJobProcessor) Initialize(ctx context.Context, jobId string, instanceId string, deleteBefore time.Time) error {
	err := p.expiredS3FilesCleanupRepo.StoreCleanupRun(ctx, entity.ExpiredS3FilesCleanupEntity{
		RunId:      jobId,
		InstanceId: instanceId,
		Status:     string(statusRunning),
		StartedAt:  time.Now(),
	})
	if err != nil {
		logger.Errorf(ctx, "Failed to initialize cleanup run: %v", err)
		return err
	}
	return nil
}

func (p *expiredS3FilesCleanupJobProcessor) Process(ctx context.Context, jobId string, deleteBefore time.Time, deletedItems *int) ([]string, error) {
	logger.Infof(ctx, "Starting cleanup of %s objects in S3 older than %s", view.BUILD_RESULT_TABLE, deleteBefore)
	deletedCount, err := p.minioStorageService.RemoveObjectsOlderThan(ctx, view.BUILD_RESULT_TABLE, deleteBefore)
	*deletedItems += deletedCount
	if err != nil {
		logger.Warnf(ctx, "Failed to remove old S3 objects: %v", err)
		return []string{fmt.Sprintf("failed to remove old S3 objects: %s", err.Error())}, err
	}

	logger.Infof(ctx, "Deleted %d expired S3 objects", deletedCount)
	return nil, nil
}
func (p *expiredS3FilesCleanupJobProcessor) UpdateProgress(ctx context.Context, jobId string, status jobStatus, errorMessage string, deletedItems int, finishedAt *time.Time) error {
	updateCtx, cancel := createContextForUpdate(ctx)
	defer cancel()

	err := p.expiredS3FilesCleanupRepo.UpdateCleanupRun(updateCtx, jobId, string(status), errorMessage, deletedItems, finishedAt)

	if err != nil {
		logger.Errorf(ctx, "failed to set '%s' status for cleanup job id %s: %s", status, jobId, err.Error())
		return err
	}
	return nil
}

func (p *expiredS3FilesCleanupJobProcessor) GetVacuumTimeout() time.Duration {
	return 0
}
func (p *expiredS3FilesCleanupJobProcessor) PerformVacuum(ctx context.Context, jobId string) error {
	return nil
}
