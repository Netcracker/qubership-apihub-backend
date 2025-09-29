// Copyright 2024-2025 NetCracker Technology Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package stages

import (
	"context"
	"fmt"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/db"
	mEntity "github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/migration/entity"
	mRepository "github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/migration/repository"
	mView "github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/migration/view"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/repository"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/service"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/utils"

	"time"

	log "github.com/sirupsen/logrus"
)

type OpsMigration struct {
	cp                     db.ConnectionProvider
	systemInfoService      service.SystemInfoService
	minioStorageService    service.MinioStorageService
	repo                   mRepository.MigrationRunRepository
	buildCleanupRepository repository.BuildCleanupRepository

	ent               mEntity.MigrationRunEntity // READ ONLY !!! Not updated during processing and contains outdated values like status, stage, etc
	keepaliveStopChan chan struct{}
	migrationCtx      context.Context
	migrationCancel   context.CancelFunc
}

func NewOpsMigration(cp db.ConnectionProvider,
	systemInfoService service.SystemInfoService,
	minioStorageService service.MinioStorageService,
	repo mRepository.MigrationRunRepository,
	buildCleanupRepository repository.BuildCleanupRepository,
	ent mEntity.MigrationRunEntity) *OpsMigration {
	ctx, cancel := context.WithCancel(context.Background())
	return &OpsMigration{
		cp:                     cp,
		systemInfoService:      systemInfoService,
		minioStorageService:    minioStorageService,
		repo:                   repo,
		buildCleanupRepository: buildCleanupRepository,
		ent:                    ent,
		keepaliveStopChan:      make(chan struct{}, 1),
		migrationCtx:           ctx,
		migrationCancel:        cancel,
	}
}

func (d OpsMigration) Start() {
	d.keepaliveWhileRunning()

	err := d.processStage(d.ent.Stage)
	if err != nil {
		log.Errorf("Migration stage failed: %s", err)
	}
}

func (d OpsMigration) processStage(stage mView.OpsMigrationStage) error {
	start := time.Now()

	if stage != mView.MigrationStageCancelling {
		// Check if migration context is cancelled before starting stage
		select {
		case <-d.migrationCtx.Done():
			log.Infof("Ops migration %s: migration cancelled, moving to cancelling stage", d.ent.Id)
			err := d.handleStageChange(mView.MigrationStageCancelling)
			if err != nil {
				return d.handleError(fmt.Errorf("ops migration %s: failed to set next stage: %s", d.ent.Id, err), stage, start)
			}
			stage = mView.MigrationStageCancelling
		default:
		}
	}

	log.Infof("Ops migration %s: processing stage %s", d.ent.Id, stage)

	var err error
	var nextStage mView.OpsMigrationStage

	// Every stage cage should be wrapped with utils.SafeSync() to handle possible panics
	switch stage {
	case mView.MigrationStageStarting:
		// Prepare for migration: create temp tables
		err = utils.SafeSync(d.StageStarting)
		if d.ent.IsRebuildChangelogOnly {
			// different sequence, rebuilding only changelogs, other stages skipped
			nextStage = mView.MigrationStageComparisonsOnly
		} else {
			// general sequence
			nextStage = mView.MigrationStageCleanupBefore
		}

	case mView.MigrationStageCleanupBefore:
		// Cleanup old migrations data to free DB space if required
		err = utils.SafeSync(d.StageCleanupBefore)
		nextStage = mView.MigrationStageIndependentVersionsLastRevs

	case mView.MigrationStageIndependentVersionsLastRevs:
		// Build latest revisions of the independent versions
		err = utils.SafeSync(d.StageIndependentVersionsLastRevisions)
		nextStage = mView.MigrationStageDependentVersionsLastRevs

	case mView.MigrationStageDependentVersionsLastRevs:
		// Iteratively build latest revisions of the dependent versions. Assuming that all independent versions are already migrated.
		err = utils.SafeSync(d.StageDependentVersionsLastRevs)
		nextStage = mView.MigrationStageIndependentVersionsOldRevs

	case mView.MigrationStageIndependentVersionsOldRevs:
		// Build old (not latest) revisions of the independent versions
		err = utils.SafeSync(d.StageIndependentVersionsOldRevisions)
		nextStage = mView.MigrationStageDependentVersionsOldRevs

	case mView.MigrationStageDependentVersionsOldRevs:
		err = utils.SafeSync(d.StageDependentVersionsOldRevs)
		nextStage = mView.MigrationStageComparisonsOther

	case mView.MigrationStageComparisonsOther:
		err = utils.SafeSync(d.StageComparisonsOther)
		nextStage = mView.MigrationStageTSRecalculate

	case mView.MigrationStageTSRecalculate:
		err = utils.SafeSync(d.StageTSRecalculate)
		nextStage = mView.MigrationStagePostCheck

	case mView.MigrationStagePostCheck:
		err = utils.SafeSync(d.StagePostCheck)
		nextStage = mView.MigrationStageDone

	case mView.MigrationStageUndefined:
		err = fmt.Errorf("ops migration FSM implementation is incorrect, migration stage = undefined")

	case mView.MigrationStageComparisonsOnly: // separate flow
		err = utils.SafeSync(d.StageComparisonsOnly)
		nextStage = mView.MigrationStagePostCheck

	case mView.MigrationStageCancelling:
		err = utils.SafeSync(d.StageCancelling)
		nextStage = mView.MigrationStageCancelled

	default:
		nextStage = mView.MigrationStageUndefined
	}

	if err != nil {
		// Check if error is due to context cancellation
		if d.migrationCtx.Err() == context.Canceled {
			log.Infof("Ops migration %s: stage %s cancelled", d.ent.Id, stage)
			nextStage = mView.MigrationStageCancelling
		} else {
			return d.handleError(err, stage, start)
		}
	} else {
		log.Infof("Ops migration %s: stage %s successfully finished. Processing took %s", d.ent.Id, stage, time.Since(start))
	}

	if nextStage == mView.MigrationStageUndefined {
		return d.handleError(fmt.Errorf("ops migration FSM implementation is incorrect, next stage was not set after '%s'", stage), stage, start)
	}

	err = d.handleStageChange(nextStage)
	if err != nil {
		return d.handleError(fmt.Errorf("ops migration %s: failed to set next stage: %s", d.ent.Id, err), stage, start)
	}

	if nextStage == mView.MigrationStageDone {
		err = d.handleComplete()
		if err != nil {
			return d.handleError(fmt.Errorf("ops migration %s: failed to run complete handler: %s", d.ent.Id, err), stage, start)
		}
		return nil
	} else if nextStage == mView.MigrationStageCancelled {
		err = d.handleCancel()
		if err != nil {
			return d.handleError(fmt.Errorf("ops migration %s: failed to run cancel handler: %s", d.ent.Id, err), stage, start)
		}
		return nil
	}

	return d.processStage(nextStage)
}

func (d OpsMigration) handleCancel() error {
	cleanupErr := d.StageCleanupAfter()
	if cleanupErr != nil {
		log.Errorf("Failed to run post-migration cleanup")
	}

	log.Infof("Ops migration %s: processing cancelled", d.ent.Id)

	_, updErr := d.cp.GetConnection().Model(&d.ent).
		Set("status=?", mView.MigrationStageCancelled).
		Set("finished_at=now()").
		Where("id = ?", d.ent.Id).Update()
	return updErr
}

func (d OpsMigration) handleError(err error, stage mView.OpsMigrationStage, start time.Time) error {
	//TODO: should we handle cancellation at this terminal stage ?
	cleanupErr := d.StageCleanupAfter()
	if cleanupErr != nil {
		log.Errorf("Failed to run post-migration cleanup")
	}

	log.Errorf("Ops migration %s: stage %s processing finished with error: %s. Processing took %s", d.ent.Id, stage, err, time.Since(start))

	_, updErr := d.cp.GetConnection().Model(&mEntity.MigrationRunEntity{}).
		Set("finished_at=now()").
		Set("status=?", mView.MigrationStatusFailed).
		Set("error_details=?", fmt.Sprintf("%s", err)).
		Where("id = ?", d.ent.Id).Update()

	return updErr
}

func (d OpsMigration) handleComplete() error {
	//TODO: should we handle cancellation at this terminal stage ?
	cleanupErr := d.StageCleanupAfter()
	if cleanupErr != nil {
		log.Errorf("Failed to run post-migration cleanup")
	}

	log.Infof("Ops migration %s: processing is successfully finished", d.ent.Id)

	_, updErr := d.cp.GetConnection().Model(&d.ent).
		Set("status=?", mView.MigrationStatusComplete).
		Set("finished_at=now()").
		Where("id = ?", d.ent.Id).Update()
	return updErr
}

func (d OpsMigration) handleStageChange(stage mView.OpsMigrationStage) error {
	_, err := d.cp.GetConnection().Model(&mEntity.MigrationRunEntity{}).
		Set("updated_at=now()").
		Set("stage=?", stage).
		Where("id = ?", d.ent.Id).Update()
	return err
}

func (d OpsMigration) keepaliveWhileRunning() {
	t := time.NewTicker(time.Second * 30)
	isCancelling := false

	utils.SafeAsync(func() {
		for {
			select {
			case <-d.keepaliveStopChan:
				log.Debugf("keepalive is stopped for migration %s", d.ent.Id)
				t.Stop()
				close(d.keepaliveStopChan)
				return
			case <-t.C:
				status := mView.MigrationStatusRunning
				if isCancelling {
					status = mView.MigrationStatusCancelling
				}
				res, err := d.cp.GetConnection().Model(&mEntity.MigrationRunEntity{}).
					Set("updated_at=now()").
					Where("id = ?", d.ent.Id).
					Where("status = ?", status).Update()
				if err != nil {
					log.Errorf("failed to update keepalive timeout for migration %s", d.ent.Id)
				}

				if res.RowsAffected() != 1 {
					log.Infof("ops migration %s: status change to not '%s' detected", d.ent.Id, status)

					var migrationEntity mEntity.MigrationRunEntity
					err := d.cp.GetConnection().Model(&migrationEntity).Where("id = ?", d.ent.Id).Select()
					if err == nil && migrationEntity.Status == mView.MigrationStatusCancelling {
						log.Infof("ops migration %s: cancelling status detected, cancelling migration context", d.ent.Id)
						d.migrationCancel()
						isCancelling = true //it is necessary to continue keepalive to avoid a restart during the cancelling stage
					} else {
						log.Infof("ops migration %s: stopping keepalive", d.ent.Id)
						d.keepaliveStopChan <- struct{}{}
					}
				}
			}
		}
	})
}
