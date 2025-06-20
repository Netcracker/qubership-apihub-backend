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

package service

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/db"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/entity"
	mRepository "github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/migration/repository"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/repository"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/utils"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
	"github.com/go-pg/pg/v10"
	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	log "github.com/sirupsen/logrus"
)

const maxErrorMessageLength = 1000

type CleanupService interface {
	ClearTestData(testId string) error
	CreateRevisionsCleanupJob(publishedRepo repository.PublishedRepository, migrationRepository mRepository.MigrationRunRepository, versionCleanupRepo repository.VersionCleanupRepository, instanceId string, schedule string, deleteLastRevision bool, deleteReleaseRevision bool, ttl int) error
}

func NewCleanupService(cp db.ConnectionProvider) CleanupService {
	return &cleanupServiceImpl{cp: cp, cron: cron.New()}
}

type cleanupServiceImpl struct {
	cp   db.ConnectionProvider
	cron *cron.Cron
}

func (c cleanupServiceImpl) ClearTestData(testId string) error {
	idFilter := "QS%-" + utils.LikeEscaped(testId) + "%"
	//clear tables: project, branch_draft_content, branch_draft_references, favorites, apihub_api_keys
	_, err := c.cp.GetConnection().Model(&entity.ProjectIntEntity{}).
		Where("id like ?", idFilter).
		ForceDelete()
	if err != nil {
		return err
	}
	//clear tables: package_group
	_, err = c.cp.GetConnection().Model(&entity.PackageEntity{}).
		Where("id like ?", idFilter).
		ForceDelete()
	if err != nil {
		return err
	}
	//clear tables: published_version, published_version_references, published_version_revision_content, published_sources
	_, err = c.cp.GetConnection().Model(&entity.PublishedVersionEntity{}).
		Where("package_id like ?", idFilter).
		ForceDelete()
	if err != nil {
		return err
	}
	//clear table: published_sources
	_, err = c.cp.GetConnection().Model(&entity.PublishedSrcEntity{}).
		Where("package_id like ?", idFilter).
		ForceDelete()
	if err != nil {
		return err
	}
	//clear table: published_sources_archives
	_, err = c.cp.GetConnection().Exec(`delete from published_sources_archives where checksum not in (select distinct archive_checksum from published_sources)`)
	if err != nil {
		return err
	}
	//clear table published_data
	_, err = c.cp.GetConnection().Model(&entity.PublishedContentDataEntity{}).
		Where("package_id like ?", idFilter).
		ForceDelete()
	if err != nil {
		return err
	}
	//clear table shared_url_info
	_, err = c.cp.GetConnection().Model(&entity.SharedUrlInfoEntity{}).
		Where("package_id like ?", idFilter).
		ForceDelete()
	if err != nil {
		return err
	}
	//clear table package_member_role
	_, err = c.cp.GetConnection().Model(&entity.PackageMemberRoleEntity{}).
		Where("user_id ilike ?", "%"+utils.LikeEscaped(testId)+"%").
		ForceDelete()
	if err != nil {
		return err
	}

	//clear personal access tokens
	_, err = c.cp.GetConnection().Model(&entity.PersonaAccessTokenEntity{}).
		Where("user_id ilike ?", "%"+utils.LikeEscaped(testId)+"%").
		ForceDelete()
	//clear table user_data
	_, err = c.cp.GetConnection().Model(&entity.UserEntity{}).
		Where("user_id ilike ?", "%"+utils.LikeEscaped(testId)+"%").
		ForceDelete()
	if err != nil {
		return err
	}
	//clear open_count tables
	_, err = c.cp.GetConnection().Exec(`delete from published_version_open_count where package_id ilike ?`, idFilter)
	if err != nil {
		return err
	}
	_, err = c.cp.GetConnection().Exec(`delete from published_document_open_count where package_id ilike ?`, idFilter)
	if err != nil {
		return err
	}
	_, err = c.cp.GetConnection().Exec(`delete from operation_open_count where package_id ilike ?`, idFilter)
	if err != nil {
		return err
	}
	//clear table version_comparison
	_, err = c.cp.GetConnection().Model(&entity.VersionComparisonEntity{}).
		Where("(package_id like ? or previous_package_id like ?)", idFilter, idFilter).
		ForceDelete()
	if err != nil {
		return err
	}
	//clear table operation_comparison
	_, err = c.cp.GetConnection().Model(&entity.OperationComparisonEntity{}).
		Where("(package_id like ? or previous_package_id like ?)", idFilter, idFilter).
		ForceDelete()
	if err != nil {
		return err
	}
	//clear table apihub_api_keys
	_, err = c.cp.GetConnection().Model(&entity.ApihubApiKeyEntity{}).
		Where("package_id like ?", idFilter).
		ForceDelete()
	if err != nil {
		return err
	}

	// TODO: need to clear business metrics as well

	return nil
}

func (c cleanupServiceImpl) CreateRevisionsCleanupJob(publishedRepository repository.PublishedRepository, migrationRepository mRepository.MigrationRunRepository, versionCleanupRepository repository.VersionCleanupRepository, instanceId string, schedule string, deleteLastRevision bool, deleteReleaseRevision bool, ttl int) error {
	job := &revisionsCleanupJob{
		cp:                       c.cp,
		publishedRepository:      publishedRepository,
		migrationRepository:      migrationRepository,
		versionCleanupRepository: versionCleanupRepository,
		instanceId:               instanceId,
		deleteLastRevision:       deleteLastRevision,
		deleteReleaseRevision:    deleteReleaseRevision,
		ttl:                      ttl,
	}
	if len(c.cron.Entries()) == 0 {
		location, err := time.LoadLocation("")
		if err != nil {
			return err
		}
		c.cron = cron.New(cron.WithLocation(location))
		c.cron.Start()
	}
	wrappedJob := cron.NewChain(cron.SkipIfStillRunning(cron.DefaultLogger)).Then(job)
	_, err := c.cron.AddJob(schedule, wrappedJob)
	if err != nil {
		log.Warnf("Revisions cleanup job wasn't added for schedule - %s. With error - %s", schedule, err)
		return err
	}
	log.Infof("Revisions cleanup job was created with schedule - %s", schedule)

	return nil
}

type revisionsCleanupJob struct {
	cp                       db.ConnectionProvider
	publishedRepository      repository.PublishedRepository
	migrationRepository      mRepository.MigrationRunRepository
	versionCleanupRepository repository.VersionCleanupRepository
	instanceId               string
	deleteLastRevision       bool
	deleteReleaseRevision    bool
	ttl                      int
}

func (j *revisionsCleanupJob) Run() {
	startTime := time.Now().Round(time.Second)
	log.Infof("Starting revisions cleanup job at %s", startTime)
	migrations, err := j.migrationRepository.GetRunningMigrations()
	if err != nil {
		log.Error("Failed to check for running migrations for build cleanup job")
		return
	}
	if len(migrations) != 0 {
		log.Infof("Revisions cleanup was skipped at %s due to migration run", startTime)
		return
	}

	jobId := uuid.New().String()
	log.Infof("Revisions cleanup job ID: %s", jobId)

	conn := j.cp.GetConnection().Conn()
	defer conn.Close()

	//TODO: rename lock name
	var lockAcquired bool
	_, err = conn.QueryOne(pg.Scan(&lockAcquired),
		`SELECT pg_try_advisory_lock(hashtext('revisions_cleanup_job'))`)
	if err != nil {
		log.Errorf("Failed to attempt lock acquisition: %v", err)
		return
	}

	if !lockAcquired {
		log.Infof("Revisions cleanup job %s skipped - another instance is already running the job", jobId)
		return
	}

	log.Infof("Revisions cleanup job %s acquired lock - proceeding with cleanup", jobId)
	defer func() {
		var lockReleased bool
		_, err := conn.QueryOne(pg.Scan(&lockReleased),
			`SELECT pg_advisory_unlock(hashtext('revisions_cleanup_job'))`)
		if err != nil {
			log.Errorf("Failed to release advisory lock: %v", err)
		} else if !lockReleased {
			log.Warnf("Failed to release advisory lock - it may have been released already")
		} else {
			log.Infof("Successfully released advisory lock for job %s", jobId)
		}
	}()

	deleteBefore := time.Now().AddDate(0, 0, -j.ttl)
	log.Debugf("[revisions cleanup] Will delete revisions older than %s (TTL: %d days)", deleteBefore, j.ttl)

	err = j.versionCleanupRepository.StoreVersionCleanupRun(entity.VersionCleanupEntity{
		RunId:        jobId,
		InstanceId:   j.instanceId,
		Status:       string(view.StatusRunning),
		PackageId:    nil,
		DeleteBefore: deleteBefore,
	})
	if err != nil {
		log.Errorf("Failed to store revisions cleanup run: %v", err)
		return
	}

	page, limit, deletedItems, errors := 0, 100, 0, []string{}
	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))

	for {
		getPackageListReq := view.PackageListReq{
			Kind:         []string{entity.KIND_PACKAGE, entity.KIND_DASHBOARD},
			Limit:        limit,
			OnlyFavorite: false,
			OnlyShared:   false,
			Offset:       page * limit,
			ParentId:     "*",
		}

		packages, err := j.publishedRepository.GetFilteredPackagesWithOffset(getPackageListReq, "")
		if err != nil {
			log.Errorf("Failed to get packages for revisions cleanup %s: %s", jobId, err.Error())
			_ = j.updateCleanupRun(jobId, string(view.StatusError), err.Error(), deletedItems)
			return
		}

		if len(packages) == 0 {
			break
		}
		log.Debugf("[revisions cleanup] Processing page %d", page+1)

		//TODO: do we really need to shuffle packages?
		// shuffle packages to randomize processing order
		rnd.Shuffle(len(packages), func(i, j int) {
			packages[i], packages[j] = packages[j], packages[i]
		})

		for idx, pkg := range packages {
			log.Debugf("[revisions cleanup] Processing package %d/%d: %s", idx+1, len(packages), pkg.Id)
			count, err := j.publishedRepository.DeletePackageRevisionsBeforeDate(pkg.Id, deleteBefore, j.deleteLastRevision, j.deleteReleaseRevision, "revisions_cleanup_job_"+jobId)
			if err != nil {
				log.Errorf("Failed to delete revisions of package %s during revisions cleanup %s: %v", pkg.Id, jobId, err)
				errors = append(errors, fmt.Sprintf("package %s: %s", pkg.Id, err.Error()))
			}
			deletedItems += count
		}
		log.Debugf("[revisions cleanup] Completed processing page %d, total deleted items so far: %d", page+1, deletedItems)
		page++
	}

	status := string(view.StatusComplete)
	errorMessage := ""
	if len(errors) > 0 {
		status = string(view.StatusError)
		errorMessage = fmt.Sprintf("Failed packages: %s", strings.Join(errors, "; "))
	}

	if err := j.updateCleanupRun(jobId, status, errorMessage, deletedItems); err != nil {
		logErrorMessage := errorMessage
		if len(logErrorMessage) > maxErrorMessageLength {
			logErrorMessage = logErrorMessage[:maxErrorMessageLength-3] + "..."
		}
		log.Errorf("Failed to save cleanup run state: %v, jobId: %s, status: %s, errorMessage: %s, deletedItems: %d", err, jobId, status, logErrorMessage, deletedItems)
		return
	}

	log.Infof("Revisions cleanup job %s finished with status '%s'. Deleted %d revisions.", jobId, status, deletedItems)
}

func (j *revisionsCleanupJob) updateCleanupRun(jobId string, status string, errorMessage string, deletedItems int) error {
	err := j.versionCleanupRepository.UpdateVersionCleanupRun(jobId, status, errorMessage, deletedItems)
	if err != nil {
		log.Errorf("failed to set '%s' status for cleanup job id %s: %s", status, jobId, err.Error())
		return err
	}
	return nil
}
