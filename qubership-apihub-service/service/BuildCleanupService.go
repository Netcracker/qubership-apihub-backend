package service

import (
	"context"
	"time"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/db"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/entity"
	mRepository "github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/migration/repository"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/repository"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	log "github.com/sirupsen/logrus"
)

const (
	// expiredS3FilesTTLDays must stay above the 30-day retention of failed builds,
	// otherwise the sweep removes files of builds that are still referenced in the database
	expiredS3FilesTTLDays = 45
	// cleanupRunUpdateTimeout bounds the write of the cleanup run result when the phase context is already cancelled
	cleanupRunUpdateTimeout = 10 * time.Second
)

type DBCleanupService interface {
	CreateCleanupJob(schedule string) error
}

func NewDBCleanupService(buildCleanUpRepository repository.BuildCleanupRepository, expiredS3FilesCleanUpRepository repository.ExpiredS3FilesCleanupRepository,
	migrationRepository mRepository.MigrationRunRepository,
	minioStorageService MinioStorageService,
	infoService SystemInfoService) DBCleanupService {
	return &dbCleanupServiceImpl{
		buildCleanUpRepository:          buildCleanUpRepository,
		expiredS3FilesCleanUpRepository: expiredS3FilesCleanUpRepository,
		migrationRepository:             migrationRepository,
		cron:                            cron.New(),
		systemInfoService:               infoService,
		minioStorageService:             minioStorageService,
	}
}

type dbCleanupServiceImpl struct {
	buildCleanUpRepository          repository.BuildCleanupRepository
	expiredS3FilesCleanUpRepository repository.ExpiredS3FilesCleanupRepository
	migrationRepository             mRepository.MigrationRunRepository
	connectionProvider              db.ConnectionProvider
	cron                            *cron.Cron
	minioStorageService             MinioStorageService
	systemInfoService               SystemInfoService
}

func (c *dbCleanupServiceImpl) CreateCleanupJob(schedule string) error {
	job := BuildCleanupJob{
		schedule:                        schedule,
		buildCleanupRepository:          c.buildCleanUpRepository,
		expiredS3FilesCleanupRepository: c.expiredS3FilesCleanUpRepository,
		minioStorageService:             c.minioStorageService,
		systemInfoService:               c.systemInfoService,
		migrationRepository:             c.migrationRepository,
	}

	if len(c.cron.Entries()) == 0 {
		location, err := time.LoadLocation("")
		if err != nil {
			return err
		}
		c.cron = cron.New(cron.WithLocation(location))
		c.cron.Start()
	}

	_, err := c.cron.AddJob(schedule, &job)
	if err != nil {
		log.Warnf("[DBCleanupService] Job wasn't added for schedule - %s. With error - %s", schedule, err)
		return err
	}
	log.Infof("[DBCleanupService] Job was created with schedule - %s", schedule)

	return nil
}

type BuildCleanupJob struct {
	schedule                        string
	buildCleanupRepository          repository.BuildCleanupRepository
	expiredS3FilesCleanupRepository repository.ExpiredS3FilesCleanupRepository
	minioStorageService             MinioStorageService
	systemInfoService               SystemInfoService
	migrationRepository             mRepository.MigrationRunRepository
}

func (j BuildCleanupJob) Run() {
	scheduledAt := time.Now().Round(time.Second)

	migrations, err := j.migrationRepository.GetRunningMigrations()
	if err != nil {
		log.Error("Failed to check for running migrations for build cleanup job")
		return
	}
	if migrations != nil && len(migrations) != 0 {
		log.Infof("Cleanup was skipped at %s due to migration run", scheduledAt)
		return
	}

	var runCleanup bool
	var lockId int
	lastCleanup, err := j.buildCleanupRepository.GetLastCleanup()
	if err != nil {
		log.Errorf("Failed to get last cleanup: %v", err)
		return
	}
	if lastCleanup != nil {
		schedule, err := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow).Parse(j.schedule)
		if err != nil {
			log.Errorf("Failed to parse schedule for cleaning job: %v", err)
			return
		}
		currentTime := time.Now().UTC()
		nextRun := schedule.Next(currentTime)
		interval := nextRun.Sub(currentTime)
		runCleanup = !lastCleanup.ScheduledAt.After(currentTime.Add(-interval))
		lockId = lastCleanup.RunId + 1
	} else {
		runCleanup = true
		lockId = 1
	}

	if runCleanup {
		log.Info("Cleanup job has started")
		err = j.buildCleanupRepository.StoreCleanup(&entity.BuildCleanupEntity{
			RunId:       lockId,
			ScheduledAt: scheduledAt,
		})
		if err != nil {
			log.Errorf("Failed to store cleanup entity: %v", err)
			return
		}
		if j.systemInfoService.IsMinioStorageActive() {
			ctx := context.Background()

			ids, err := j.buildCleanupRepository.GetRemoveCandidateOldBuildEntitiesIds()
			if err != nil {
				log.Errorf("Failed to get up remove candidate old build ids: %v", err)
				return
			}
			if len(ids) == 0 {
				log.Info("No old build entities to clean up")
			} else {
				err = j.minioStorageService.RemoveFiles(ctx, view.BUILD_RESULT_TABLE, ids)
				if err != nil {
					log.Errorf("Failed to remove old build results from minio storage: %v", err)
					return
				}

				err = j.buildCleanupRepository.RemoveOldBuildSourcesByIds(ctx, ids, lockId, scheduledAt)
				if err != nil {
					log.Errorf("Failed to clean up old builds sources: %v", err)
					return
				}
			}

			j.cleanupExpiredS3Files(ctx)
		} else {
			err = j.buildCleanupRepository.RemoveOldBuildEntities(lockId, scheduledAt)
			if err != nil {
				log.Errorf("Failed to clean up old builds: %v", err)
				return
			}
		}

		cleanupEnt, err := j.buildCleanupRepository.GetCleanup(lockId)
		if err != nil {
			log.Errorf("Failed to get cleanup run entity with id %d", lockId)
			return
		}
		log.Infof("Cleanup was performed at %s with results: %v", scheduledAt, *cleanupEnt)
	} else {
		log.Infof("Cleanup was skipped at %s", scheduledAt)
	}
}

func (j BuildCleanupJob) cleanupExpiredS3Files(ctx context.Context) {
	timeout := time.Duration(j.systemInfoService.GetExpiredS3FilesCleanupTimeout()) * time.Minute
	phaseCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	runId := uuid.New().String()
	if err := j.expiredS3FilesCleanupRepository.StoreCleanupRun(phaseCtx, entity.ExpiredS3FilesCleanupEntity{
		RunId:     runId,
		StartedAt: time.Now(),
	}); err != nil {
		log.Errorf("Failed to store expired s3 files cleanup run: %v", err)
		return
	}

	olderThan := time.Now().AddDate(0, 0, -expiredS3FilesTTLDays)
	deletedCount, err := j.minioStorageService.RemoveObjectsOlderThan(phaseCtx, view.BUILD_RESULT_TABLE, olderThan)
	finishedAt := time.Now()
	errMessage := ""
	if err != nil {
		log.Errorf("Failed to remove old S3 objects: %v", err)
		errMessage = err.Error()
	}

	updateCtx, updateCancel := contextForCleanupRunUpdate(phaseCtx)
	defer updateCancel()
	if err := j.expiredS3FilesCleanupRepository.UpdateCleanupRun(updateCtx, runId, errMessage, deletedCount, &finishedAt); err != nil {
		log.Errorf("Failed to update expired s3 files cleanup run: %v", err)
	}

	log.Infof("Deleted %d expired S3 objects", deletedCount)
}

func contextForCleanupRunUpdate(parentCtx context.Context) (context.Context, context.CancelFunc) {
	if parentCtx.Err() != nil {
		return context.WithTimeout(context.Background(), cleanupRunUpdateTimeout)
	}
	return parentCtx, func() {}
}
