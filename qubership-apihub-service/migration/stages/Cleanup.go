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
			ids, err := d.buildCleanupRepository.GetRemoveMigrationBuildIds(d.migrationCtx)
			if err != nil {
				return err
			}
			err = d.minioStorageService.RemoveFiles(d.migrationCtx, view.BUILD_RESULT_TABLE, ids)
			if err != nil {
				return err
			}
			deleted, err := d.buildCleanupRepository.RemoveMigrationBuildSourceData(d.migrationCtx, ids)
			if err != nil {
				return err
			}
			log.Infof("ops migration %s: Cleanup before full migration cleaned up %d entries", d.ent.Id, deleted)
		} else {
			deleted, err := d.buildCleanupRepository.RemoveMigrationBuildData(d.migrationCtx)
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
