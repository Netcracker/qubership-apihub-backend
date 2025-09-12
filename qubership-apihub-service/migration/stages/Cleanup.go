package stages

import (
	"context"
	"fmt"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"

	log "github.com/sirupsen/logrus"
)

func (d OpsMigration) StageCleanupBefore() error {
	if len(d.ent.PackageIds) == 0 && len(d.ent.Versions) == 0 {
		// it means that we're going to rebuild all versions
		// this action will generate a lot of data and may cause DB disk overflow
		// Try to avoid too much space usage by cleaning up all old migration build data
		log.Infof("ops migration %s: Starting cleanup before full migration", d.ent.Id)
		if d.systemInfoService.IsMinioStorageActive() {
			ctx := context.Background()
			ids, err := d.buildCleanupRepository.GetRemoveMigrationBuildIds()
			if err != nil {
				return err
			}
			err = d.minioStorageService.RemoveFiles(ctx, view.BUILD_RESULT_TABLE, ids)
			if err != nil {
				return err
			}
			deleted, err := d.buildCleanupRepository.RemoveMigrationBuildSourceData(ids)
			if err != nil {
				return err
			}
			log.Infof("ops migration %s: Cleanup before full migration cleaned up %d entries", d.ent.Id, deleted)
		} else {
			deleted, err := d.buildCleanupRepository.RemoveMigrationBuildData()
			if err != nil {
				return err
			}
			log.Infof("ops migration %s: Cleanup before full migration cleaned up %d entries", d.ent.Id, deleted)
		}
	}
	return nil
}

func (d OpsMigration) StageCleanupAfter() error {
	// delete temporary tables after migration end
	_, err := d.cp.GetConnection().Exec(fmt.Sprintf(`drop table migration."version_comparison_%s";`, d.ent.Id))
	if err != nil {
		log.Errorf("failed to cleanup migration tables: %v", err.Error())
	}
	_, err = d.cp.GetConnection().Exec(fmt.Sprintf(`drop table migration."operation_comparison_%s";`, d.ent.Id))
	if err != nil {
		log.Errorf("failed to cleanup migration tables: %v", err.Error())
	}
	_, err = d.cp.GetConnection().Exec(fmt.Sprintf(`drop table migration."expired_ts_operation_data_%s";`, d.ent.Id))
	if err != nil {
		log.Errorf("ops migration %s: failed to cleanup migration tables: %v", d.ent.Id, err.Error())
	}
	return nil
}
