package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/db"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/entity"
	"github.com/go-pg/pg/v10"
	log "github.com/sirupsen/logrus"
)

type UnreferencedDataCleanupRepository interface {
	StoreCleanupRun(ctx context.Context, entity entity.UnreferencedDataCleanupEntity) error
	UpdateCleanupRun(ctx context.Context, runId string, status string, details string, finishedAt *time.Time) error

	DeleteUnreferencedOperationData(ctx context.Context, runId string, batchSize int) (int, error)
	DeleteUnreferencedOperationGroupTemplates(ctx context.Context, runId string, batchSize int) (int, error)
	DeleteUnreferencedSrcArchives(ctx context.Context, runId string, batchSize int) (int, error)
	DeleteUnreferencedPublishedData(ctx context.Context, runId string, batchSize int) (int, error)
	VacuumAffectedTables(ctx context.Context, runId string) error
}

func NewUnreferencedDataCleanupRepository(cp db.ConnectionProvider) UnreferencedDataCleanupRepository {
	return &unreferencedDataCleanupRepositoryImpl{cp: cp}
}

type unreferencedDataCleanupRepositoryImpl struct {
	cp db.ConnectionProvider
}

func (u unreferencedDataCleanupRepositoryImpl) StoreCleanupRun(ctx context.Context, entity entity.UnreferencedDataCleanupEntity) error {
	_, err := u.cp.GetConnection().ModelContext(ctx, &entity).Insert()
	return err
}

func (u unreferencedDataCleanupRepositoryImpl) UpdateCleanupRun(ctx context.Context, runId string, status string, details string, finishedAt *time.Time) error {
	query := u.cp.GetConnection().ModelContext(ctx, &entity.UnreferencedDataCleanupEntity{})

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

func (u unreferencedDataCleanupRepositoryImpl) DeleteUnreferencedOperationData(ctx context.Context, runId string, batchSize int) (int, error) {
	var deletedItems entity.DeletedItemsCounts

	err := u.cp.GetConnection().RunInTransaction(ctx, func(tx *pg.Tx) error {
		getUnreferencedDataHashQuery := `
			SELECT od.data_hash
			FROM operation_data od
			WHERE NOT EXISTS (
    			SELECT 1 FROM operation o WHERE o.data_hash = od.data_hash
			)
			ORDER BY od.data_hash
			LIMIT ?`

		var dataHash []string
		_, err := tx.QueryContext(ctx, &dataHash, getUnreferencedDataHashQuery, batchSize)
		if err != nil {
			return fmt.Errorf("failed to get unreferenced data hash: %w", err)
		}

		if len(dataHash) == 0 {
			return nil
		}
		log.Debugf("[unreferenced data cleanup] Found %d operation data entities to delete in current batch", len(dataHash))

		err = u.countRelatedDataForOperationDataTx(ctx, tx, dataHash, &deletedItems)
		if err != nil {
			return fmt.Errorf("failed to count operation data related data: %w", err)
		}

		log.Tracef("[unreferenced data cleanup] Deleting operation data with hash: %v", dataHash)
		deleteOperationDataQuery := `
			DELETE FROM operation_data
			WHERE data_hash IN (?)`
		_, err = tx.ExecContext(ctx, deleteOperationDataQuery, pg.In(dataHash))
		if err != nil {
			return fmt.Errorf("failed to delete operation data: %w", err)
		}

		deletedItems.OperationData = len(dataHash)

		var cleanupRun entity.UnreferencedDataCleanupEntity
		err = tx.Model(&cleanupRun).
			Where("run_id = ?", runId).
			Select()
		if err != nil {
			return fmt.Errorf("failed to get current state of cleanup run: %w", err)
		}
		if cleanupRun.DeletedItems == nil {
			cleanupRun.DeletedItems = &deletedItems
		} else {
			cleanupRun.DeletedItems.OperationData += deletedItems.OperationData
			cleanupRun.DeletedItems.TSGQLOperationData += deletedItems.TSGQLOperationData
			cleanupRun.DeletedItems.TSRestOperationData += deletedItems.TSRestOperationData
			cleanupRun.DeletedItems.TSOperationData += deletedItems.TSOperationData
			cleanupRun.DeletedItems.FTSOperationData += deletedItems.FTSOperationData
		}
		_, err = tx.Model(&cleanupRun).
			Column("deleted_items").
			WherePK().
			Update()
		if err != nil {
			return fmt.Errorf("failed to update cleanup run state: %w", err)
		}

		return nil
	})

	return deletedItems.OperationData, err
}

func (u unreferencedDataCleanupRepositoryImpl) countRelatedDataForOperationDataTx(ctx context.Context, tx *pg.Tx, dataHash []string, deletedItems *entity.DeletedItemsCounts) error {
	_, err := tx.QueryOneContext(ctx, pg.Scan(&deletedItems.TSGQLOperationData),
		`SELECT COUNT(*) FROM ts_graphql_operation_data WHERE data_hash IN (?)`, pg.In(dataHash))
	if err != nil {
		return err
	}

	_, err = tx.QueryOneContext(ctx, pg.Scan(&deletedItems.TSRestOperationData),
		`SELECT COUNT(*) FROM ts_rest_operation_data WHERE data_hash IN (?)`, pg.In(dataHash))
	if err != nil {
		return err
	}

	_, err = tx.QueryOneContext(ctx, pg.Scan(&deletedItems.TSOperationData),
		`SELECT COUNT(*) FROM ts_operation_data WHERE data_hash IN (?)`, pg.In(dataHash))
	if err != nil {
		return err
	}

	_, err = tx.QueryOneContext(ctx, pg.Scan(&deletedItems.FTSOperationData),
		`SELECT COUNT(*) FROM fts_operation_data WHERE data_hash IN (?)`, pg.In(dataHash))
	if err != nil {
		return err
	}

	return nil
}

func (u unreferencedDataCleanupRepositoryImpl) DeleteUnreferencedOperationGroupTemplates(ctx context.Context, runId string, batchSize int) (int, error) {
	var deletedTemplates int

	err := u.cp.GetConnection().RunInTransaction(ctx, func(tx *pg.Tx) error {
		deleteUnreferencedOperationGroupTemplatesQuery := `
			WITH to_delete AS (
        		SELECT ogt.checksum
				FROM operation_group_template ogt
				WHERE NOT EXISTS (
    				SELECT 1 FROM operation_group og WHERE og.template_checksum = ogt.checksum
				)
				ORDER BY ogt.checksum
        		LIMIT ?
    		)
    		DELETE FROM operation_group_template
    		WHERE checksum IN (SELECT checksum FROM to_delete)`
		res, err := tx.ExecContext(ctx, deleteUnreferencedOperationGroupTemplatesQuery, batchSize)
		if err != nil {
			return fmt.Errorf("failed to delete unreferenced operation group templates: %w", err)
		}

		log.Debugf("[unreferenced data cleanup] Deleted %d operation group templates in current batch", res.RowsAffected())
		if res.RowsAffected() == 0 {
			return nil
		}
		deletedTemplates = res.RowsAffected()

		var cleanupRun entity.UnreferencedDataCleanupEntity
		err = tx.Model(&cleanupRun).
			Where("run_id = ?", runId).
			Select()
		if err != nil {
			return fmt.Errorf("failed to get current state of cleanup run: %w", err)
		}
		if cleanupRun.DeletedItems == nil {
			cleanupRun.DeletedItems = &entity.DeletedItemsCounts{}
		}
		cleanupRun.DeletedItems.OperationGroupTemplate += deletedTemplates
		_, err = tx.Model(&cleanupRun).
			Column("deleted_items").
			WherePK().
			Update()
		if err != nil {
			return fmt.Errorf("failed to update cleanup run state: %w", err)
		}

		return nil
	})

	return deletedTemplates, err
}

func (u unreferencedDataCleanupRepositoryImpl) DeleteUnreferencedSrcArchives(ctx context.Context, runId string, batchSize int) (int, error) {
	var deletedArchives int

	err := u.cp.GetConnection().RunInTransaction(ctx, func(tx *pg.Tx) error {
		deleteUnreferencedSrcArchivesQuery := `
			WITH to_delete AS (
        		SELECT psa.checksum
				FROM published_sources_archives psa
				WHERE NOT EXISTS (
    				SELECT 1 FROM published_sources ps WHERE ps.archive_checksum = psa.checksum
				)
				ORDER BY psa.checksum
        		LIMIT ?
    		)
    		DELETE FROM published_sources_archives
    		WHERE checksum IN (SELECT checksum FROM to_delete)`
		res, err := tx.ExecContext(ctx, deleteUnreferencedSrcArchivesQuery, batchSize)
		if err != nil {
			return fmt.Errorf("failed to delete unreferenced source archives: %w", err)
		}

		log.Debugf("[unreferenced data cleanup] Deleted %d unreferenced source archives in current batch", res.RowsAffected())
		if res.RowsAffected() == 0 {
			return nil
		}
		deletedArchives = res.RowsAffected()

		var cleanupRun entity.UnreferencedDataCleanupEntity
		err = tx.Model(&cleanupRun).
			Where("run_id = ?", runId).
			Select()
		if err != nil {
			return fmt.Errorf("failed to get current state of cleanup run: %w", err)
		}
		if cleanupRun.DeletedItems == nil {
			cleanupRun.DeletedItems = &entity.DeletedItemsCounts{}
		}
		cleanupRun.DeletedItems.PublishedSrcArchives += deletedArchives
		_, err = tx.Model(&cleanupRun).
			Column("deleted_items").
			WherePK().
			Update()
		if err != nil {
			return fmt.Errorf("failed to update cleanup run state: %w", err)
		}

		return nil
	})

	return deletedArchives, err
}

func (u unreferencedDataCleanupRepositoryImpl) DeleteUnreferencedPublishedData(ctx context.Context, runId string, batchSize int) (int, error) {
	var deletedPublishData int

	err := u.cp.GetConnection().RunInTransaction(ctx, func(tx *pg.Tx) error {
		deleteUnreferencedPublishDataQuery := `
			WITH to_delete AS (
        		SELECT pd.checksum
				FROM published_data pd
				WHERE NOT EXISTS (
    				SELECT 1 FROM published_version_revision_content pvrc WHERE pvrc.checksum = pd.checksum
				)
				ORDER BY pd.checksum
        		LIMIT ?
    		)
    		DELETE FROM published_data
    		WHERE checksum IN (SELECT checksum FROM to_delete)`
		res, err := tx.ExecContext(ctx, deleteUnreferencedPublishDataQuery, batchSize)
		if err != nil {
			return fmt.Errorf("failed to delete unreferenced publish data: %w", err)
		}

		log.Debugf("[unreferenced data cleanup] Deleted %d unreferenced publish data in current batch", res.RowsAffected())
		if res.RowsAffected() == 0 {
			return nil
		}
		deletedPublishData = res.RowsAffected()

		var cleanupRun entity.UnreferencedDataCleanupEntity
		err = tx.Model(&cleanupRun).
			Where("run_id = ?", runId).
			Select()
		if err != nil {
			return fmt.Errorf("failed to get current state of cleanup run: %w", err)
		}
		if cleanupRun.DeletedItems == nil {
			cleanupRun.DeletedItems = &entity.DeletedItemsCounts{}
		}
		cleanupRun.DeletedItems.PublishedData += deletedPublishData
		_, err = tx.Model(&cleanupRun).
			Column("deleted_items").
			WherePK().
			Update()
		if err != nil {
			return fmt.Errorf("failed to update cleanup run state: %w", err)
		}

		return nil
	})

	return deletedPublishData, err
}

func (u unreferencedDataCleanupRepositoryImpl) VacuumAffectedTables(ctx context.Context, runId string) error {
	var cleanupEntity entity.UnreferencedDataCleanupEntity
	err := u.cp.GetConnection().ModelContext(ctx, &cleanupEntity).
		Where("run_id = ?", runId).
		Select()
	if err != nil {
		return err
	}

	var vacuumErrors []string

	if cleanupEntity.DeletedItems != nil {
		deletedItems := cleanupEntity.DeletedItems
		if deletedItems.OperationData > 0 {
			log.Debugf("Vacuuming 'operation_data' table for %d deleted entries (runId=%s)", deletedItems.OperationData, runId)
			_, err = u.cp.GetConnection().ExecContext(ctx, "VACUUM FULL operation_data")
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to vacuum 'operation_data' table: %v", err)
				log.Warn(errorMsg)
				vacuumErrors = append(vacuumErrors, errorMsg)
			} else {
				log.Tracef("Successfully vacuumed 'operation_data' table for cleanup run %s", runId)
			}
		}
		if deletedItems.TSGQLOperationData > 0 {
			log.Debugf("Vacuuming 'ts_graphql_operation_data' table for %d deleted entries (runId=%s)", deletedItems.TSGQLOperationData, runId)
			_, err = u.cp.GetConnection().ExecContext(ctx, "VACUUM FULL ts_graphql_operation_data")
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to vacuum 'ts_graphql_operation_data' table: %v", err)
				log.Warn(errorMsg)
				vacuumErrors = append(vacuumErrors, errorMsg)
			} else {
				log.Tracef("Successfully vacuumed 'ts_graphql_operation_data' table for cleanup run %s", runId)
			}
		}
		if deletedItems.TSRestOperationData > 0 {
			log.Debugf("Vacuuming 'ts_rest_operation_data' table for %d deleted entries (runId=%s)", deletedItems.TSRestOperationData, runId)
			_, err = u.cp.GetConnection().ExecContext(ctx, "VACUUM FULL ts_rest_operation_data")
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to vacuum 'ts_rest_operation_data' table: %v", err)
				log.Warn(errorMsg)
				vacuumErrors = append(vacuumErrors, errorMsg)
			} else {
				log.Tracef("Successfully vacuumed 'ts_rest_operation_data' table for cleanup run %s", runId)
			}
		}
		if deletedItems.TSOperationData > 0 {
			log.Debugf("Vacuuming 'ts_operation_data' table for %d deleted entries (runId=%s)", deletedItems.TSOperationData, runId)
			_, err = u.cp.GetConnection().ExecContext(ctx, "VACUUM FULL ts_operation_data")
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to vacuum 'ts_operation_data' table: %v", err)
				log.Warn(errorMsg)
				vacuumErrors = append(vacuumErrors, errorMsg)
			} else {
				log.Tracef("Successfully vacuumed 'ts_operation_data' table for cleanup run %s", runId)
			}
		}
		if deletedItems.OperationGroupTemplate > 0 {
			log.Debugf("Vacuuming 'operation_group_template' table for %d deleted entries (runId=%s)", deletedItems.OperationGroupTemplate, runId)
			_, err = u.cp.GetConnection().ExecContext(ctx, "VACUUM FULL operation_group_template")
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to vacuum 'operation_group_template' table: %v", err)
				log.Warn(errorMsg)
				vacuumErrors = append(vacuumErrors, errorMsg)
			} else {
				log.Tracef("Successfully vacuumed 'operation_group_template' table for cleanup run %s", runId)
			}
		}
		if deletedItems.PublishedSrcArchives > 0 {
			log.Debugf("Vacuuming 'published_sources_archives' table for %d deleted entries (runId=%s)", deletedItems.PublishedSrcArchives, runId)
			_, err = u.cp.GetConnection().ExecContext(ctx, "VACUUM FULL published_sources_archives")
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to vacuum 'published_sources_archives' table: %v", err)
				log.Warn(errorMsg)
				vacuumErrors = append(vacuumErrors, errorMsg)
			} else {
				log.Tracef("Successfully vacuumed 'published_sources_archives' table for cleanup run %s", runId)
			}
		}
		if deletedItems.PublishedData > 0 {
			log.Debugf("Vacuuming 'published_data' table for %d deleted build results (runId=%s)", deletedItems.PublishedData, runId)
			_, err = u.cp.GetConnection().ExecContext(ctx, "VACUUM FULL published_data")
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to vacuum 'published_data' table: %v", err)
				log.Warn(errorMsg)
				vacuumErrors = append(vacuumErrors, errorMsg)
			} else {
				log.Tracef("Successfully vacuumed 'published_data' table for cleanup run %s", runId)
			}
		}
	} else {
		log.Infof("No deleted items found for cleanup run %s - skipping vacuum operations", runId)
	}

	if len(vacuumErrors) > 0 {
		log.Errorf("Vacuum operations completed with %d errors for cleanup run %s: %s",
			len(vacuumErrors), runId, strings.Join(vacuumErrors, "; "))
		return fmt.Errorf("vacuum operations failed: %s", strings.Join(vacuumErrors, "; "))
	}

	return nil
}
