package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/metrics"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/repository"
	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	log "github.com/sirupsen/logrus"
)

const (
	aiChatRetentionLockName    = "ai_chat_retention_cleanup"
	aiChatFilesCleanupLockName = "ai_chat_files_cleanup"

	aiChatCleanupLockLeaseSeconds     = 120
	aiChatCleanupLockHeartbeatSeconds = 30

	aiChatExpiredFilesBatchSize = 500
)

// AiChatCleanupService schedules two background jobs:
//   - chat retention (TTL + last-N + pinned protection)
//   - generated files cleanup (DB rows past expires_at and FS unlink)
type AiChatCleanupService interface {
	StartChatRetentionJob(schedule string, retentionDays, pinnedForeverCount int) error
	StartGeneratedFilesCleanupJob(schedule string, generatedFilesDir string) error
}

func NewAiChatCleanupService(repo repository.AiChatRepository, lockService LockService) AiChatCleanupService {
	return &aiChatCleanupServiceImpl{
		repo:        repo,
		lockService: lockService,
	}
}

type aiChatCleanupServiceImpl struct {
	repo        repository.AiChatRepository
	lockService LockService
	cron        *cron.Cron
	started     bool
}

func (s *aiChatCleanupServiceImpl) StartChatRetentionJob(schedule string, retentionDays, pinnedForeverCount int) error {
	if strings.TrimSpace(schedule) == "" {
		log.Info("[AiChatCleanup] chat retention job not scheduled (empty schedule)")
		return nil
	}
	job := &chatRetentionJob{
		repo:               s.repo,
		lockService:        s.lockService,
		retentionDays:      retentionDays,
		pinnedForeverCount: pinnedForeverCount,
	}
	return s.addJob(schedule, job, "ai-chat retention")
}

func (s *aiChatCleanupServiceImpl) StartGeneratedFilesCleanupJob(schedule string, generatedFilesDir string) error {
	if strings.TrimSpace(schedule) == "" {
		log.Info("[AiChatCleanup] generated files cleanup not scheduled (empty schedule)")
		return nil
	}
	job := &generatedFilesCleanupJob{
		repo:        s.repo,
		lockService: s.lockService,
		baseDir:     generatedFilesDir,
	}
	return s.addJob(schedule, job, "ai-chat generated files cleanup")
}

func (s *aiChatCleanupServiceImpl) addJob(schedule string, job cron.Job, label string) error {
	if !s.started {
		location, err := time.LoadLocation("")
		if err != nil {
			return err
		}
		s.cron = cron.New(cron.WithLocation(location))
		s.cron.Start()
		s.started = true
	}
	wrapped := cron.NewChain(cron.SkipIfStillRunning(cron.DefaultLogger)).Then(job)
	if _, err := s.cron.AddJob(schedule, wrapped); err != nil {
		log.Warnf("[AiChatCleanup] %s job not scheduled (%s): %v", label, schedule, err)
		return err
	}
	log.Infof("[AiChatCleanup] %s job scheduled with %s", label, schedule)
	return nil
}

// withAiChatLock acquires the named distributed lock and runs body with a derived context.
// The body is responsible for honoring ctx cancellation.
func withAiChatLock(parentCtx context.Context, ls LockService, lockName string, body func(ctx context.Context) error) error {
	opts := LockOptions{
		LeaseSeconds:             aiChatCleanupLockLeaseSeconds,
		HeartbeatIntervalSeconds: aiChatCleanupLockHeartbeatSeconds,
		NotifyOnLoss:             true,
	}
	acquired, lostCh, err := ls.AcquireLock(parentCtx, lockName, opts)
	if err != nil {
		return err
	}
	if !acquired {
		return errAiChatLockBusy
	}
	bodyCtx, cancel := context.WithCancel(parentCtx)
	defer cancel()
	if lostCh != nil {
		go func() {
			ev, ok := <-lostCh
			if !ok {
				return
			}
			log.Warnf("[AiChatCleanup] lock %s lost: %s", ev.LockName, ev.Reason)
			cancel()
		}()
	}
	defer func() {
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer releaseCancel()
		if rErr := ls.ReleaseLock(releaseCtx, lockName); rErr != nil {
			log.Warnf("[AiChatCleanup] release lock %s: %v", lockName, rErr)
		}
	}()
	return body(bodyCtx)
}

var errAiChatLockBusy = errors.New("ai-chat cleanup lock is held by another instance")

// chatRetentionJob deletes expired non-pinned chats per user, while keeping the most recent
// pinnedForeverCount of them and never touching pinned ones.
type chatRetentionJob struct {
	repo               repository.AiChatRepository
	lockService        LockService
	retentionDays      int
	pinnedForeverCount int
}

func (j *chatRetentionJob) Run() {
	jobID := uuid.NewString()
	if j.retentionDays < 1 {
		log.Debugf("[AiChatCleanup] retention job %s skipped (retentionDays<1)", jobID)
		return
	}
	parent, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	err := withAiChatLock(parent, j.lockService, aiChatRetentionLockName, func(ctx context.Context) error {
		userIDs, err := j.repo.ListUserIDs(ctx)
		if err != nil {
			return err
		}
		var deletedTotal, processed, errs int
		for _, uid := range userIDs {
			if ctx.Err() != nil {
				break
			}
			n, err := j.repo.DeleteUserChatsByRetention(ctx, uid, j.retentionDays, j.pinnedForeverCount)
			if err != nil {
				errs++
				log.Warnf("[AiChatCleanup] retention failed for user %s: %v", uid, err)
				continue
			}
			processed++
			deletedTotal += n
		}
		if deletedTotal > 0 {
			metrics.AiChatCleanupDeleted.WithLabelValues("retention", "chat").Add(float64(deletedTotal))
		}
		log.Infof("[AiChatCleanup] retention job %s done: users=%d deleted=%d errors=%d", jobID, processed, deletedTotal, errs)
		return nil
	})
	if err == errAiChatLockBusy {
		log.Debugf("[AiChatCleanup] retention job %s skipped (lock busy)", jobID)
		return
	}
	if err != nil {
		log.Warnf("[AiChatCleanup] retention job %s error: %v", jobID, err)
	}
}

// generatedFilesCleanupJob removes DB rows past expires_at and unlinks the matching FS file.
type generatedFilesCleanupJob struct {
	repo        repository.AiChatRepository
	lockService LockService
	baseDir     string
}

func (j *generatedFilesCleanupJob) Run() {
	jobID := uuid.NewString()
	parent, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	err := withAiChatLock(parent, j.lockService, aiChatFilesCleanupLockName, func(ctx context.Context) error {
		var rmFs, rmDb, errs int
		for {
			if ctx.Err() != nil {
				break
			}
			rows, err := j.repo.ListExpiredFiles(ctx, aiChatExpiredFilesBatchSize)
			if err != nil {
				return err
			}
			if len(rows) == 0 {
				break
			}
			for i := range rows {
				if ctx.Err() != nil {
					break
				}
				row := rows[i]
				if !isPathSafe(j.baseDir, row.StoragePath) {
					log.Warnf("[AiChatCleanup] refusing to remove path outside base dir: %q", row.StoragePath)
				} else if row.StoragePath != "" {
					if err := os.Remove(row.StoragePath); err != nil && !os.IsNotExist(err) {
						errs++
						log.Warnf("[AiChatCleanup] unlink %s: %v", row.StoragePath, err)
					} else {
						rmFs++
					}
				}
				if err := j.repo.DeleteFileByID(ctx, row.ID); err != nil {
					errs++
					log.Warnf("[AiChatCleanup] delete row %s: %v", row.ID, err)
					continue
				}
				rmDb++
			}
			if len(rows) < aiChatExpiredFilesBatchSize {
				break
			}
		}
		if rmDb > 0 {
			metrics.AiChatCleanupDeleted.WithLabelValues("files", "row").Add(float64(rmDb))
		}
		if rmFs > 0 {
			metrics.AiChatCleanupDeleted.WithLabelValues("files", "fs").Add(float64(rmFs))
		}
		log.Infof("[AiChatCleanup] files job %s done: removedFromDB=%d unlinked=%d errors=%d", jobID, rmDb, rmFs, errs)
		return nil
	})
	if err == errAiChatLockBusy {
		log.Debugf("[AiChatCleanup] files job %s skipped (lock busy)", jobID)
		return
	}
	if err != nil {
		log.Warnf("[AiChatCleanup] files job %s error: %v", jobID, err)
	}
}

// isPathSafe ensures path is anchored under baseDir; empty baseDir disables the check.
// This is a defense-in-depth guard so that a corrupted DB row cannot trigger arbitrary FS removal.
func isPathSafe(baseDir, p string) bool {
	if strings.TrimSpace(baseDir) == "" {
		return true
	}
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return false
	}
	absPath, err := filepath.Abs(p)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absBase, absPath)
	if err != nil {
		return false
	}
	if strings.HasPrefix(rel, "..") {
		return false
	}
	return true
}
