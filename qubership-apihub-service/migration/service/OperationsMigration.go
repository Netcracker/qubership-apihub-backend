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
	"context"
	"fmt"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/exception"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/migration/stages"
	"github.com/google/uuid"
	"net/http"
	"strconv"
	"strings"
	"time"

	mEntity "github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/migration/entity"
	mView "github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/migration/view"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/utils"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
	"github.com/go-pg/pg/v10"
	log "github.com/sirupsen/logrus"
)

func (d *dbMigrationServiceImpl) StartMigrateOperations(req mView.MigrationRequest) (string, error) {
	migrationId := uuid.New().String()

	log.Infof("Starting migration with request: %+v, generated id = %s", req, migrationId)

	var om *stages.OpsMigration

	err := d.cp.GetConnection().RunInTransaction(context.Background(), func(tx *pg.Tx) error {
		if len(req.PackageIds) == 0 && len(req.Versions) == 0 {
			// Allow only one full migration. Full migration have no limitations for packageIds and versions, i.e. all(non-deleted) data will be migrated.
			var ents []mEntity.MigrationRunEntity
			/*_, err := tx.Query(&ents, "select * from migration_run where array_length(package_ids,1) is null "+
			"and array_length(versions,1) is null and status=? for update", mView.MigrationStatusRunning) // TODO what about rebuild params?*/
			err := tx.Model(&ents).Where("status=?", mView.MigrationStatusRunning).Select()

			if err != nil {
				return err
			}

			if len(ents) > 0 {
				return &exception.CustomError{
					Status:  http.StatusConflict,
					Code:    exception.OperationsMigrationConflict,
					Message: exception.OperationsMigrationConflictMsg,
					Params:  map[string]interface{}{"reason": "full migration is already running"},
				}
			}
		}

		mrEnt := mEntity.MigrationRunEntity{
			Id:                     migrationId,
			StartedAt:              time.Now(),
			Status:                 mView.MigrationStatusRunning,
			Stage:                  mView.MigrationStageStarting,
			PackageIds:             req.PackageIds,
			Versions:               req.Versions,
			IsRebuild:              req.Rebuild,
			CurrentBuilderVersion:  req.CurrentBuilderVersion,
			IsRebuildChangelogOnly: req.RebuildChangelogOnly,
			SkipValidation:         req.SkipValidation,
			InstanceId:             d.instanceId,
		}

		// TODO: add optimistic lock for insert??

		_, err := d.cp.GetConnection().Model(&mrEnt).Insert()
		if err != nil {
			return fmt.Errorf("failed to insert MigrationRunEntity: %w", err)
		}

		om = stages.NewOpsMigration(d.cp, d.systemInfoService, d.minioStorageService, d.repo, d.buildCleanupRepository, mrEnt)

		return nil
	})
	if err != nil {
		return "", err
	}

	utils.SafeAsync(func() {
		if om != nil {
			om.Start()
		} else {
			log.Errorf("OpsMigration is nil!!!")
		}

		/*err := d.migrateOperations(migrationId, req)
		if err != nil {
			log.Errorf("Operations migration process failed: %s", err)
		} else {
			log.Infof("Operations migration process complete")
		}*/
	})

	return migrationId, err
}

func (d dbMigrationServiceImpl) GetMigrationReport(migrationId string, includeBuildSamples bool) (*mView.MigrationReport, error) {
	mRunEnt, err := d.repo.GetMigrationRun(migrationId)
	if mRunEnt == nil {
		return nil, fmt.Errorf("migration with id=%s not found", migrationId)
	}

	result := mView.MigrationReport{
		Status:             mRunEnt.Status,
		StartedAt:          mRunEnt.StartedAt,
		ElapsedTime:        time.Since(mRunEnt.StartedAt).String(),
		SuccessBuildsCount: 0,
		ErrorBuildsCount:   0,
		ErrorBuilds:        nil,
	}
	if !mRunEnt.FinishedAt.IsZero() {
		result.ElapsedTime = mRunEnt.FinishedAt.Sub(mRunEnt.StartedAt).String()
		result.FinishedAt = &mRunEnt.FinishedAt
	}

	// TODO: reimplement!

	var migratedVersions []mEntity.MigratedVersionResultEntity
	err = d.cp.GetConnection().Model(&migratedVersions).
		ColumnExpr(`migrated_version.*,
					b.metadata->>'previous_version' previous_version,
					b.metadata->>'previous_version_package_id' previous_version_package_id`).
		Join("inner join build b").
		JoinOn("migrated_version.build_id = b.build_id").
		Where("migrated_version.migration_id = ?", migrationId).
		Select()

	for _, mv := range migratedVersions {
		if mv.Error != "" {
			result.ErrorBuilds = append(result.ErrorBuilds, mView.MigrationError{
				PackageId:                mv.PackageId,
				Version:                  mv.Version,
				Revision:                 mv.Revision,
				Error:                    mv.Error,
				BuildId:                  mv.BuildId,
				BuildType:                mv.BuildType,
				PreviousVersion:          mv.PreviousVersion,
				PreviousVersionPackageId: mv.PreviousVersionPackageId,
			})

			result.ErrorBuildsCount += 1
		} else {
			result.SuccessBuildsCount += 1
		}
	}

	migrationChanges := make(map[string]int)
	_, err = d.cp.GetConnection().Query(pg.Scan(&migrationChanges), `select changes from migration_changes where migration_id = ?`, migrationId)

	for change, count := range migrationChanges {
		migrationChange := mView.MigrationChange{
			ChangedField:        change,
			AffectedBuildsCount: count,
		}
		if includeBuildSamples {
			changedVersion := new(mEntity.MigratedVersionChangesResultEntity)
			err = d.cp.GetConnection().Model(changedVersion).
				ColumnExpr(`migrated_version_changes.*,
						b.metadata->>'build_type' build_type,
						b.metadata->>'previous_version' previous_version,
						b.metadata->>'previous_version_package_id' previous_version_package_id`).
				Join("inner join build b").
				JoinOn("migrated_version_changes.build_id = b.build_id").
				Where("migrated_version_changes.migration_id = ?", migrationId).
				Where("? = any(unique_changes)", change).
				Order("build_id").
				Limit(1).
				Select()
			migrationChange.AffectedBuildSample = mEntity.MakeSuspiciousBuildView(*changedVersion)
		}
		result.MigrationChanges = append(result.MigrationChanges, migrationChange)
	}
	_, err = d.cp.GetConnection().Query(pg.Scan(&result.SuspiciousBuildsCount),
		`select count(*) from migrated_version_changes where migration_id = ?`, migrationId)

	return &result, err
}

func (d dbMigrationServiceImpl) GetSuspiciousBuilds(migrationId string, changedField string, limit int, page int) ([]mView.SuspiciousMigrationBuild, error) {
	changedVersions := make([]mEntity.MigratedVersionChangesResultEntity, 0)
	err := d.cp.GetConnection().Model(&changedVersions).
		ColumnExpr(`migrated_version_changes.*,
				b.metadata->>'build_type' build_type,
				b.metadata->>'previous_version' previous_version,
				b.metadata->>'previous_version_package_id' previous_version_package_id`).
		Join("inner join build b").
		JoinOn("migrated_version_changes.build_id = b.build_id").
		Where("migrated_version_changes.migration_id = ?", migrationId).
		Where("(? = '') or (? = any(unique_changes))", changedField, changedField).
		Order("build_id").
		Limit(limit).
		Offset(limit * page).
		Select()
	if err != nil {
		return nil, err
	}
	suspiciousBuilds := make([]mView.SuspiciousMigrationBuild, 0)
	for _, changedVersion := range changedVersions {
		suspiciousBuilds = append(suspiciousBuilds, *mEntity.MakeSuspiciousBuildView(changedVersion))
	}
	return suspiciousBuilds, nil
}

func (d dbMigrationServiceImpl) updateMigrationStatus(migrationId string, status string, stage mView.OpsMigrationStage) error {
	mEnt, err := d.repo.GetMigrationRun(migrationId)
	if err != nil {
		return err
	}
	if status != "" {
		if status == mView.MigrationStatusComplete || status == mView.MigrationStatusFailed {
			mEnt.FinishedAt = time.Now()
		}
		mEnt.Status = status
	}
	if stage != "" {
		mEnt.Stage = stage
	}
	return d.repo.UpdateMigrationRun(mEnt)
}

func (d dbMigrationServiceImpl) rebuildAllChangelogs(packageIds []string, versionsIn []string, migrationId string) error {
	changelogQuery := makeAllChangelogForMigrationQuery(packageIds, versionsIn)
	var migrationChangelogEntities []mEntity.MigrationChangelogEntity

	_, err := queryWithRetry(d.cp.GetConnection(), &migrationChangelogEntities, changelogQuery)
	if err != nil {
		log.Errorf("Failed to get migrationChangelogEntities: %v", err.Error())
		return err
	}
	err = d.rebuildChangelog(migrationChangelogEntities, migrationId)
	if err != nil {
		log.Errorf("Failed to rebuildChangelog: %v", err.Error())
		return err
	}
	return nil
}

func (d dbMigrationServiceImpl) rebuildChangelogsAfterVersionsMigrations(migrationId string) error {
	changelogQuery := makeChangelogByMigratedVersionQuery(migrationId)
	var migrationChangelogEntities []mEntity.MigrationChangelogEntity
	_, err := queryWithRetry(d.cp.GetConnection(), &migrationChangelogEntities, changelogQuery)
	if err != nil {
		log.Errorf("Failed to get migrationChangelogEntities: %v", err.Error())
		return err
	}
	err = d.rebuildChangelog(migrationChangelogEntities, migrationId)
	if err != nil {
		log.Errorf("Failed to rebuildChangelog: %v", err.Error())
		return err
	}
	return nil
}

func (d dbMigrationServiceImpl) rebuildChangelog(migrationChangelogs []mEntity.MigrationChangelogEntity, migrationId string) error {
	err := d.updateMigrationStatus(migrationId, "", "rebuildChangelogs_start")
	if err != nil {
		return err
	}

	buildsMap := make(map[string]interface{}, 0)
	err = d.updateMigrationStatus(migrationId, "", "rebuildChangelogs_adding_builds")
	if err != nil {
		return err
	}
	for _, changelogEntity := range migrationChangelogs {
		buildId, err := d.addChangelogTaskToRebuild(migrationId, changelogEntity)
		if err != nil {
			log.Errorf("Failed to add task to rebuild changelog. Package - %s. Version - %s. Revision - %d.Error - %v", changelogEntity.PackageId, changelogEntity.Version, changelogEntity.Revision, err.Error())
			mvEnt := mEntity.MigratedVersionEntity{
				PackageId:   changelogEntity.PackageId,
				Version:     changelogEntity.Version,
				Revision:    changelogEntity.Revision,
				Error:       fmt.Sprintf("addChangelogTaskToRebuild failed: %v", err.Error()),
				BuildId:     buildId,
				MigrationId: migrationId,
				BuildType:   view.ChangelogType,
			}
			_, err = d.cp.GetConnection().Model(&mvEnt).Insert()
			if err != nil {
				log.Errorf("Failed to store error for %v@%v@%v : %s", changelogEntity.PackageId, changelogEntity.Version, changelogEntity.Revision, err.Error())
				continue
			}
		}
		buildsMap[buildId] = changelogEntity
		log.Infof("addChangelogTaskToRebuild end. BuildId: %s", buildId)
	}
	err = d.updateMigrationStatus(migrationId, "", "rebuildChangelogs_waiting_builds")
	if err != nil {
		return err
	}
	log.Info("Waiting for all builds to finish.")
	buildsThisRound := len(buildsMap)
	finishedBuilds := 0
	migrationCancelled := false
MigrationProcess:
	for len(buildsMap) > 0 {
		log.Infof("Finished builds: %v / %v.", finishedBuilds, buildsThisRound)
		time.Sleep(15 * time.Second)
		buildIdsList := getMapKeysGeneric(buildsMap)
		buildEnts, err := d.getBuilds(buildIdsList)
		if err != nil {
			log.Errorf("Failed to get builds statuses: %v", err.Error())
			return err
		}
		for _, buildEnt := range buildEnts {
			buildVersion := strings.Split(buildEnt.Version, "@")[0]
			buildRevision := strings.Split(buildEnt.Version, "@")[1]
			buildPackageId := buildEnt.PackageId

			buildRevisionInt := 1

			mvEnt := mEntity.MigratedVersionEntity{
				PackageId:   buildPackageId,
				Version:     buildVersion,
				Revision:    buildRevisionInt,
				Error:       "",
				BuildId:     buildEnt.BuildId,
				MigrationId: migrationId,
				BuildType:   view.ChangelogType,
			}

			if buildRevision != "" {
				buildRevisionInt, err = strconv.Atoi(buildRevision)
				if err != nil {
					mvEnt.Error = fmt.Sprintf("Unable to convert revision value '%s' to int", buildRevision)
					_, err = d.cp.GetConnection().Model(&mvEnt).Insert()
					if err != nil {
						log.Errorf("failed to store MigratedVersionEntity %+v: %s", mvEnt, err)
					}
					continue
				}
				mvEnt.Revision = buildRevisionInt
			}

			if buildEnt.Status == string(view.StatusComplete) {
				finishedBuilds = finishedBuilds + 1
				delete(buildsMap, buildEnt.BuildId)
				_, err = d.cp.GetConnection().Model(&mvEnt).Insert()
				if err != nil {
					log.Errorf("failed to store MigratedVersionEntity %+v: %s", mvEnt, err)
				}
				continue
			}
			if buildEnt.Status == string(view.StatusError) {
				if buildEnt.Details == CancelledMigrationError {
					migrationCancelled = true
					break MigrationProcess
				}

				finishedBuilds = finishedBuilds + 1

				errorDetails := buildEnt.Details
				if errorDetails == "" {
					errorDetails = "No error details.."
				}

				delete(buildsMap, buildEnt.BuildId)

				log.Errorf("Builder failed to build %v. Details: %v", buildEnt.BuildId, errorDetails)

				mvEnt.Error = errorDetails

				_, err = d.cp.GetConnection().Model(&mvEnt).Insert()
				if err != nil {
					log.Errorf("failed to store MigratedVersionEntity %+v: %s", mvEnt, err)
				}
				continue
			}
		}
	}
	log.Info("Finished rebuilding changelogs")
	if migrationCancelled {
		return fmt.Errorf(CancelledMigrationError)
	}
	return nil
}

func (d dbMigrationServiceImpl) rebuildTextSearchTables(migrationId string) error {
	err := d.updateMigrationStatus(migrationId, "", "rebuildTextSearchTables_start")
	if err != nil {
		return err
	}
	log.Info("Start rebuilding text search tables for changed search scopes")

	log.Info("Calculating ts_rest_operation_data")
	calculateRestTextSearchDataQuery := fmt.Sprintf(`
	insert into ts_rest_operation_data
		select data_hash,
		to_tsvector(jsonb_extract_path_text(search_scope, ?)) scope_request,
		to_tsvector(jsonb_extract_path_text(search_scope, ?)) scope_response,
		to_tsvector(jsonb_extract_path_text(search_scope, ?)) scope_annotation,
		to_tsvector(jsonb_extract_path_text(search_scope, ?)) scope_properties,
		to_tsvector(jsonb_extract_path_text(search_scope, ?)) scope_examples
		from operation_data
		where data_hash in (
			select distinct o.data_hash
			from operation o
			inner join migration."expired_ts_operation_data_%s"  exp
			on exp.package_id = o.package_id
			and exp.version = o.version
			and exp.revision = o.revision
			where o.type = ?
		)
        order by 1
        for update skip locked
	on conflict (data_hash) do update
	set scope_request = EXCLUDED.scope_request,
	scope_response = EXCLUDED.scope_response,
	scope_annotation = EXCLUDED.scope_annotation,
	scope_properties = EXCLUDED.scope_properties,
	scope_examples = EXCLUDED.scope_examples;`, migrationId)
	_, err = d.cp.GetConnection().Exec(calculateRestTextSearchDataQuery,
		view.RestScopeRequest, view.RestScopeResponse, view.RestScopeAnnotation, view.RestScopeProperties, view.RestScopeExamples,
		view.RestApiType)
	if err != nil {
		return fmt.Errorf("failed to calculate ts_rest_operation_data: %w", err)
	}

	log.Info("Calculating ts_graphql_operation_data")
	calculateGraphqlTextSearchDataQuery := fmt.Sprintf(`
	insert into ts_graphql_operation_data
		select data_hash,
		to_tsvector(jsonb_extract_path_text(search_scope, ?)) scope_argument,
		to_tsvector(jsonb_extract_path_text(search_scope, ?)) scope_property,
		to_tsvector(jsonb_extract_path_text(search_scope, ?)) scope_annotation
		from operation_data
		where data_hash in (
			select distinct o.data_hash
			from operation o
			inner join migration."expired_ts_operation_data_%s"  exp
			on exp.package_id = o.package_id
			and exp.version = o.version
			and exp.revision = o.revision
			where o.type = ?
		)
        order by 1
        for update skip locked
	on conflict (data_hash) do update
	set scope_argument = EXCLUDED.scope_argument,
	scope_property = EXCLUDED.scope_property,
	scope_annotation = EXCLUDED.scope_annotation;`, migrationId)
	_, err = d.cp.GetConnection().Exec(calculateGraphqlTextSearchDataQuery,
		view.GraphqlScopeArgument, view.GraphqlScopeProperty, view.GraphqlScopeAnnotation,
		view.GraphqlApiType)
	if err != nil {
		return fmt.Errorf("failed to calculate ts_grahpql_operation_data: %w", err)
	}

	log.Info("Calculating ts_operation_data")
	calculateAllTextSearchDataQuery := fmt.Sprintf(`
	insert into ts_operation_data
		select data_hash,
		to_tsvector(jsonb_extract_path_text(search_scope, ?)) scope_all
		from operation_data
		where data_hash in (
			select distinct o.data_hash
			from operation o
			inner join migration."expired_ts_operation_data_%s"  exp
			on exp.package_id = o.package_id
			and exp.version = o.version
			and exp.revision = o.revision
		)
        order by 1
        for update skip locked
	on conflict (data_hash) do update
	set scope_all = EXCLUDED.scope_all`, migrationId)
	_, err = d.cp.GetConnection().Exec(calculateAllTextSearchDataQuery, view.ScopeAll)
	if err != nil {
		return fmt.Errorf("failed to calculate ts_operation_data: %w", err)
	}
	log.Info("Finished rebuilding text search tables for changed search scopes")
	err = d.updateMigrationStatus(migrationId, "", "rebuildTextSearchTables_end")
	if err != nil {
		return err
	}
	return nil
}
