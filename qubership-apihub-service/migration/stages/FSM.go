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
	// TODO: handle migration cancelled somehow!
}

func NewOpsMigration(cp db.ConnectionProvider,
	systemInfoService service.SystemInfoService,
	minioStorageService service.MinioStorageService,
	repo mRepository.MigrationRunRepository,
	buildCleanupRepository repository.BuildCleanupRepository,
	ent mEntity.MigrationRunEntity) *OpsMigration {
	return &OpsMigration{
		cp:                     cp,
		systemInfoService:      systemInfoService,
		minioStorageService:    minioStorageService,
		repo:                   repo,
		buildCleanupRepository: buildCleanupRepository,
		ent:                    ent,
		keepaliveStopChan:      make(chan struct{}, 1),
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
			//TODO: should we perform cleanup before this stage as well ?
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

	default:
		nextStage = mView.MigrationStageUndefined
	}

	if err != nil {
		return d.handleError(err, stage, start)
	}

	log.Infof("Ops migration %s: stage %s successfully finished. Processing took %s", d.ent.Id, stage, time.Since(start))

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
	}

	return d.processStage(nextStage)
}

func (d OpsMigration) handleError(err error, stage mView.OpsMigrationStage, start time.Time) error {
	cleanupErr := d.StageCleanupAfter()
	if cleanupErr != nil {
		log.Errorf("Failed to run post-migration cleanup")
	}

	d.keepaliveStopChan <- struct{}{}

	log.Errorf("Ops migration %s: stage %s processing finished with error: %s. Processing took %s", d.ent.Id, stage, err, time.Since(start))

	_, updErr := d.cp.GetConnection().Model(&mEntity.MigrationRunEntity{}).
		Set("finished_at=now()").
		Set("status=?", mView.MigrationStatusFailed).
		Set("error_details=?", fmt.Sprintf("%s", err)).
		Where("id = ?", d.ent.Id).Update()

	return updErr
}

func (d OpsMigration) handleComplete() error {
	err := d.StageCleanupAfter()
	if err != nil {
		log.Errorf("Failed to run post-migration cleanup")
	}

	d.keepaliveStopChan <- struct{}{}

	log.Infof("Ops migration %s: processing is successfully finished", d.ent.Id)

	_, err = d.cp.GetConnection().Model(&d.ent).
		Set("status=?", mView.MigrationStatusComplete).
		Set("finished_at=now()").
		Where("id = ?", d.ent.Id).Update()
	return err
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

	utils.SafeAsync(func() {
		for {
			select {
			case <-d.keepaliveStopChan:
				log.Debugf("keepalive is stopped for migration %s", d.ent.Id)
				t.Stop()
				return
			case <-t.C:
				res, err := d.cp.GetConnection().Model(&mEntity.MigrationRunEntity{}).
					Set("updated_at=now()").
					Where("id = ?", d.ent.Id).
					Where("status = ?", mView.MigrationStatusRunning).Update()
				if err != nil {
					log.Errorf("failed to update keepalive timeout for migration %s", d.ent.Id)
				}

				if res.RowsAffected() != 1 {
					log.Infof("ops migration %s: status change to not running detected. Stopping keepalive", d.ent.Id)
					d.keepaliveStopChan <- struct{}{}
				}
			}
		}
		// TODO: maybe handle migration cancel here???
	})
}
