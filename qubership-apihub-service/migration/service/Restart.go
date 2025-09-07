package service

import (
	"context"
	"fmt"
	mEntity "github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/migration/entity"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/migration/stages"
	mView "github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/migration/view"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/utils"

	"github.com/go-pg/pg/v10"
)

func (d *dbMigrationServiceImpl) restartMigrations() error {
	var om *stages.OpsMigration

	err := d.cp.GetConnection().RunInTransaction(context.Background(), func(tx *pg.Tx) error {
		var ents []mEntity.MigrationRunEntity
		_, err := tx.Query(&ents, `select * from migration_run where status=? and
			 updated_at < (now() - interval '? seconds') order by started_at for update skip locked `, mView.MigrationStatusRunning, 120)
		if err != nil {
			return err
		}
		if len(ents) == 0 {
			return nil
		}
		// ok, we have stale migrations, take the oldest one
		mrEnt := ents[0]

		// TODO: add retries!!!

		_, err = tx.Model(&mEntity.MigrationRunEntity{}).Set("instance_id=?", d.instanceId).Set("updated_at=now()").Where("id=?", mrEnt.Id).Update()
		if err != nil {
			return fmt.Errorf("failed to update migration %s for restart: %w", mrEnt.Id, err)
		}

		mrEnt.InstanceId = d.instanceId
		om = stages.NewOpsMigration(d.cp, d.systemInfoService, d.minioStorageService, d.repo, d.buildCleanupRepository, mrEnt)

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to run restartMigrations(): %w", err)
	}

	if om != nil {
		// restart the migration
		utils.SafeAsync(func() {
			om.Start()
		})
	}

	return nil
}
