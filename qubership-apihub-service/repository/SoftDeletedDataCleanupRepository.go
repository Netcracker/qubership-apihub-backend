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

package repository

import (
	"context"
	"fmt"
	log "github.com/sirupsen/logrus"
	"strings"
	"time"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/db"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/entity"
)

type SoftDeletedDataCleanupRepository interface {
	StoreCleanupRun(ctx context.Context, entity entity.SoftDeletedDataCleanupEntity) error
	UpdateCleanupRun(ctx context.Context, runId string, status string, details string, finishedAt *time.Time) error
	VacuumAffectedTables(ctx context.Context, runId string) error
}

func NewDeletedDataCleanupRepository(cp db.ConnectionProvider) SoftDeletedDataCleanupRepository {
	return &deletedDataCleanupRepositoryImpl{cp: cp}
}

type deletedDataCleanupRepositoryImpl struct {
	cp db.ConnectionProvider
}

func (d deletedDataCleanupRepositoryImpl) StoreCleanupRun(ctx context.Context, entity entity.SoftDeletedDataCleanupEntity) error {
	_, err := d.cp.GetConnection().ModelContext(ctx, &entity).Insert()
	return err
}

func (d deletedDataCleanupRepositoryImpl) UpdateCleanupRun(ctx context.Context, runId string, status string, details string, finishedAt *time.Time) error {
	query := d.cp.GetConnection().ModelContext(ctx, &entity.SoftDeletedDataCleanupEntity{})

	if status != "" {
		query = query.Set("status=?", status)
	}

	if details != "" {
		query = query.Set("details=?", details)
	}

	if finishedAt != nil {
		query = query.Set("finished_at=?", finishedAt)
	}

	_, err := query.Where("run_id = ?", runId).Update()
	return err
}

func (d deletedDataCleanupRepositoryImpl) VacuumAffectedTables(ctx context.Context, runId string) error {
	var cleanupEntity entity.SoftDeletedDataCleanupEntity
	err := d.cp.GetConnection().ModelContext(ctx, &cleanupEntity).
		Where("run_id = ?", runId).
		Select()
	if err != nil {
		return err
	}

	var vacuumErrors []string

	if cleanupEntity.DeletedItems != nil && cleanupEntity.DeletedItems.TotalRecords > 0 {
		deletedItems := cleanupEntity.DeletedItems
		if len(deletedItems.Packages) > 0 {
			_, err = d.cp.GetConnection().ExecContext(ctx, "VACUUM FULL package_group")
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to vacuum 'package_group' table: %v", err)
				log.Warn(errorMsg)
				vacuumErrors = append(vacuumErrors, errorMsg)
			}
		}
		if len(deletedItems.PackageRevisions) > 0 {
			_, err = d.cp.GetConnection().ExecContext(ctx, "VACUUM FULL published_version")
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to vacuum 'published_version' table: %v", err)
				log.Warn(errorMsg)
				vacuumErrors = append(vacuumErrors, errorMsg)
			}
		}
		if deletedItems.ActivityTracking > 0 {
			_, err = d.cp.GetConnection().ExecContext(ctx, "VACUUM FULL activity_tracking")
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to vacuum 'activity_tracking' table: %v", err)
				log.Warn(errorMsg)
				vacuumErrors = append(vacuumErrors, errorMsg)
			}
		}
		if len(deletedItems.ApiKeys) > 0 {
			_, err = d.cp.GetConnection().ExecContext(ctx, "VACUUM FULL apihub_api_keys")
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to vacuum 'apihub_api_keys' table: %v", err)
				log.Warn(errorMsg)
				vacuumErrors = append(vacuumErrors, errorMsg)
			}
		}
		if deletedItems.Builds > 0 {
			_, err = d.cp.GetConnection().ExecContext(ctx, "VACUUM FULL build")
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to vacuum 'build' table: %v", err)
				log.Warn(errorMsg)
				vacuumErrors = append(vacuumErrors, errorMsg)
			}
		}
		if deletedItems.BuildDepends > 0 {
			_, err = d.cp.GetConnection().ExecContext(ctx, "VACUUM FULL build_depends")
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to vacuum 'build_depends' table: %v", err)
				log.Warn(errorMsg)
				vacuumErrors = append(vacuumErrors, errorMsg)
			}
		}
		if deletedItems.BuildResults > 0 {
			_, err = d.cp.GetConnection().ExecContext(ctx, "VACUUM FULL build_result")
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to vacuum 'build_result' table: %v", err)
				log.Warn(errorMsg)
				vacuumErrors = append(vacuumErrors, errorMsg)
			}
		}
		if deletedItems.BuildSources > 0 {
			_, err = d.cp.GetConnection().ExecContext(ctx, "VACUUM FULL build_src")
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to vacuum 'build_src' table: %v", err)
				log.Warn(errorMsg)
				vacuumErrors = append(vacuumErrors, errorMsg)
			}
		}
		if deletedItems.BuilderNotifications > 0 {
			_, err = d.cp.GetConnection().ExecContext(ctx, "VACUUM FULL builder_notifications")
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to vacuum 'builder_notifications' table: %v", err)
				log.Warn(errorMsg)
				vacuumErrors = append(vacuumErrors, errorMsg)
			}
		}
		if deletedItems.FavoritePackages > 0 {
			_, err = d.cp.GetConnection().ExecContext(ctx, "VACUUM FULL favorite_packages")
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to vacuum 'favorite_packages' table: %v", err)
				log.Warn(errorMsg)
				vacuumErrors = append(vacuumErrors, errorMsg)
			}
		}
		if deletedItems.MigratedVersions > 0 {
			_, err = d.cp.GetConnection().ExecContext(ctx, "VACUUM FULL migrated_version")
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to vacuum 'migrated_version' table: %v", err)
				log.Warn(errorMsg)
				vacuumErrors = append(vacuumErrors, errorMsg)
			}
		}
		if deletedItems.Operations > 0 {
			_, err = d.cp.GetConnection().ExecContext(ctx, "VACUUM FULL operation")
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to vacuum 'operation' table: %v", err)
				log.Warn(errorMsg)
				vacuumErrors = append(vacuumErrors, errorMsg)
			}
		}
		if deletedItems.OperationGroups > 0 {
			_, err = d.cp.GetConnection().ExecContext(ctx, "VACUUM FULL operation_group")
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to vacuum 'operation_group' table: %v", err)
				log.Warn(errorMsg)
				vacuumErrors = append(vacuumErrors, errorMsg)
			}
		}
		if deletedItems.GroupedOperations > 0 {
			_, err = d.cp.GetConnection().ExecContext(ctx, "VACUUM FULL grouped_operation")
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to vacuum 'grouped_operation' table: %v", err)
				log.Warn(errorMsg)
				vacuumErrors = append(vacuumErrors, errorMsg)
			}
		}
		if deletedItems.OperationOpenCounts > 0 {
			_, err = d.cp.GetConnection().ExecContext(ctx, "VACUUM FULL operation_open_count")
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to vacuum 'operation_open_count' table: %v", err)
				log.Warn(errorMsg)
				vacuumErrors = append(vacuumErrors, errorMsg)
			}
		}
		if deletedItems.PackageExportConfigs > 0 {
			_, err = d.cp.GetConnection().ExecContext(ctx, "VACUUM FULL package_export_config")
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to vacuum 'package_export_config' table: %v", err)
				log.Warn(errorMsg)
				vacuumErrors = append(vacuumErrors, errorMsg)
			}
		}
		if len(deletedItems.PackageMembersRoles) > 0 {
			_, err = d.cp.GetConnection().ExecContext(ctx, "VACUUM FULL package_member_role")
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to vacuum 'package_member_role' table: %v", err)
				log.Warn(errorMsg)
				vacuumErrors = append(vacuumErrors, errorMsg)
			}
		}
		if len(deletedItems.PackageServices) > 0 {
			_, err = d.cp.GetConnection().ExecContext(ctx, "VACUUM FULL package_service")
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to vacuum 'package_service' table: %v", err)
				log.Warn(errorMsg)
				vacuumErrors = append(vacuumErrors, errorMsg)
			}
		}
		if deletedItems.PackageTransitions > 0 {
			_, err = d.cp.GetConnection().ExecContext(ctx, "VACUUM FULL package_transition")
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to vacuum 'package_transition' table: %v", err)
				log.Warn(errorMsg)
				vacuumErrors = append(vacuumErrors, errorMsg)
			}
		}
		if deletedItems.PublishedData > 0 {
			_, err = d.cp.GetConnection().ExecContext(ctx, "VACUUM FULL published_data")
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to vacuum 'published_data' table: %v", err)
				log.Warn(errorMsg)
				vacuumErrors = append(vacuumErrors, errorMsg)
			}
		}
		if deletedItems.PublishedDocumentOpenCounts > 0 {
			_, err = d.cp.GetConnection().ExecContext(ctx, "VACUUM FULL published_document_open_count")
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to vacuum 'published_document_open_count' table: %v", err)
				log.Warn(errorMsg)
				vacuumErrors = append(vacuumErrors, errorMsg)
			}
		}
		if deletedItems.PublishedSources > 0 {
			_, err = d.cp.GetConnection().ExecContext(ctx, "VACUUM FULL published_sources")
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to vacuum 'published_sources' table: %v", err)
				log.Warn(errorMsg)
				vacuumErrors = append(vacuumErrors, errorMsg)
			}
		}
		if deletedItems.PublishedVersionOpenCounts > 0 {
			_, err = d.cp.GetConnection().ExecContext(ctx, "VACUUM FULL published_version_open_count")
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to vacuum 'published_version_open_count' table: %v", err)
				log.Warn(errorMsg)
				vacuumErrors = append(vacuumErrors, errorMsg)
			}
		}
		if deletedItems.PublishedVersionReferences > 0 {
			_, err = d.cp.GetConnection().ExecContext(ctx, "VACUUM FULL published_version_reference")
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to vacuum 'published_version_reference' table: %v", err)
				log.Warn(errorMsg)
				vacuumErrors = append(vacuumErrors, errorMsg)
			}
		}
		if deletedItems.PublishedVersionRevisionContent > 0 {
			_, err = d.cp.GetConnection().ExecContext(ctx, "VACUUM FULL published_version_revision_content")
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to vacuum 'published_version_revision_content' table: %v", err)
				log.Warn(errorMsg)
				vacuumErrors = append(vacuumErrors, errorMsg)
			}
		}
		if deletedItems.PublishedVersionValidation > 0 {
			_, err = d.cp.GetConnection().ExecContext(ctx, "VACUUM FULL published_version_validation")
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to vacuum 'published_version_validation' table: %v", err)
				log.Warn(errorMsg)
				vacuumErrors = append(vacuumErrors, errorMsg)
			}
		}
		if deletedItems.SharedUrlInfo > 0 {
			_, err = d.cp.GetConnection().ExecContext(ctx, "VACUUM FULL shared_url_info")
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to vacuum 'shared_url_info' table: %v", err)
				log.Warn(errorMsg)
				vacuumErrors = append(vacuumErrors, errorMsg)
			}
		}
		if deletedItems.TransformedContentData > 0 {
			_, err = d.cp.GetConnection().ExecContext(ctx, "VACUUM FULL transformed_content_data")
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to vacuum 'transformed_content_data' table: %v", err)
				log.Warn(errorMsg)
				vacuumErrors = append(vacuumErrors, errorMsg)
			}
		}
	}

	if len(vacuumErrors) > 0 {
		return fmt.Errorf("vacuum operations failed: %s", strings.Join(vacuumErrors, "; "))
	}

	return nil
}
