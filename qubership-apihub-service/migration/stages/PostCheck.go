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

	"strings"

	mEntity "github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/migration/entity"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
)

// StagePostCheck check that migration affected all required versions and comparisons!
func (d OpsMigration) StagePostCheck() error {
	// self-check

	var wherePackageIn string
	if len(d.ent.PackageIds) > 0 {
		wherePackageIn = " and v.package_id in ("
		for i, pkg := range d.ent.PackageIds {
			if i > 0 {
				wherePackageIn += ","
			}
			wherePackageIn += fmt.Sprintf("'%s'", pkg)
		}
		wherePackageIn += ") "
	}

	var whereVersionIn string
	if len(d.ent.Versions) > 0 {
		whereVersionIn = " and v.version in ("
		for i, ver := range d.ent.Versions {
			if i > 0 {
				whereVersionIn += ","
			}
			verSplit := strings.Split(ver, "@")
			whereVersionIn += fmt.Sprintf("'%s'", verSplit[0])
		}
		whereVersionIn += ") "
	}

	postCheckResult := &mEntity.PostCheckResultEntity{
		NotMigratedVersions:    make([]mEntity.MigrationVersionEntity, 0),
		NotMigratedComparisons: make([]mEntity.MigrationChangelogEntity, 0),
	}

	if !d.ent.IsRebuildChangelogOnly {
		notMigratedVersionsQuery := fmt.Sprintf(`
			select v.package_id, v.version, v.revision, v.previous_version, v.previous_version_package_id from published_version v
			inner join package_group pkg on v.package_id = pkg.id
			where v.deleted_at is null and pkg.deleted_at is null
			  and not exists(
				select 1 from build b
				where b.package_id = v.package_id
				  and (string_to_array(b.version, '@'))[1] = v.version
				  and (string_to_array(b.version, '@'))[2]::int = v.revision
				  and b.metadata->>'build_type' = 'build'
				  and (b.status='%s' or b.status='%s')
				  and b.metadata->>'migration_id' = '%s'
			  ) %s %s`, view.StatusComplete, view.StatusError, d.ent.Id, wherePackageIn, whereVersionIn)

		_, err := d.cp.GetConnection().Query(&postCheckResult.NotMigratedVersions, notMigratedVersionsQuery)
		if err != nil {
			return fmt.Errorf("failed to query not migrated versions: %v", err.Error())
		}
		//TODO: do we need also find not migrated changelogs (they are built with versions) ?
		//find not migrated ad-hoc comparisons
		notMigratedComparisonsQuery := fmt.Sprintf(`
		select vc.package_id, vc.version, vc.revision, vc.previous_package_id, vc.previous_version, vc.previous_revision from version_comparison vc
		inner join published_version pv1 on vc.package_id=pv1.package_id and vc.version=pv1.version and vc.revision=pv1.revision
		inner join published_version pv2 on vc.previous_package_id=pv2.package_id and vc.previous_version=pv2.version and vc.previous_revision=pv2.revision
		inner join package_group pg1 on vc.package_id=pg1.id
		inner join package_group pg2 on vc.previous_package_id=pg2.id
		where pv1.deleted_at is null and pv2.deleted_at is null and pg1.deleted_at is null and pg2.deleted_at is null
		and (vc.previous_package_id!=(CASE WHEN (pv1.previous_version_package_id IS NULL OR pv1.previous_version_package_id = '') THEN pv1.package_id ELSE pv1.previous_version_package_id END)
		or vc.previous_version!=coalesce(pv1.previous_version, ''))
		and not exists(
		    select 1 from build b
		    where b.package_id = vc.package_id
		      and (string_to_array(b.version, '@'))[1] = vc.version
		      and (string_to_array(b.version, '@'))[2]::int = vc.revision
		      and b.metadata->>'build_type' = 'changelog'
		      and b.metadata->>'previous_version' = vc.previous_version || '@' || vc.previous_revision
		      and b.metadata->>'previous_version_package_id' = vc.previous_package_id
		      and (b.status='%s' or b.status='%s')
		      and b.metadata->>'migration_id' = '%s'
		  ) %s %s`, view.StatusComplete, view.StatusError, d.ent.Id, wherePackageIn, whereVersionIn)

		_, err = d.cp.GetConnection().Query(&postCheckResult.NotMigratedComparisons, notMigratedComparisonsQuery)
		if err != nil {
			return fmt.Errorf("failed to query not migrated comparisons: %v", err.Error())
		}
	} else {
		notMigratedComparisonsQuery := fmt.Sprintf(`
		select vc.package_id, vc.version, vc.revision, vc.previous_package_id, vc.previous_version, vc.previous_revision from version_comparison vc
		inner join published_version pv1 on vc.package_id=pv1.package_id and vc.version=pv1.version and vc.revision=pv1.revision
		inner join published_version pv2 on vc.previous_package_id=pv2.package_id and vc.previous_version=pv2.version and vc.previous_revision=pv2.revision
		inner join package_group pg1 on vc.package_id=pg1.id
		inner join package_group pg2 on vc.previous_package_id=pg2.id
		where pv1.deleted_at is null and pv2.deleted_at is null and pg1.deleted_at is null and pg2.deleted_at is null
		  and not exists(
		    select 1 from build b
		    where b.package_id = vc.package_id
		      and (string_to_array(b.version, '@'))[1] = vc.version
		      and (string_to_array(b.version, '@'))[2]::int = vc.revision
		      and b.metadata->>'build_type' = 'changelog'
		      and b.metadata->>'previous_version' = vc.previous_version || '@' || vc.previous_revision
		      and b.metadata->>'previous_version_package_id' = vc.previous_package_id
		      and (b.status='%s' or b.status='%s')
		      and b.metadata->>'migration_id' = '%s'
		  ) %s %s`, view.StatusComplete, view.StatusError, d.ent.Id, wherePackageIn, whereVersionIn)

		_, err := d.cp.GetConnection().Query(&postCheckResult.NotMigratedComparisons, notMigratedComparisonsQuery)
		if err != nil {
			return fmt.Errorf("failed to query not migrated comparisons: %v", err.Error())
		}
	}

	if len(postCheckResult.NotMigratedVersions) > 0 || len(postCheckResult.NotMigratedComparisons) > 0 {
		_, err := d.cp.GetConnection().Model(&mEntity.MigrationRunEntity{}).
			Set("post_check_result = ?", postCheckResult).
			Where("id = ?", d.ent.Id).Update()
		if err != nil {
			return fmt.Errorf("failed to store post-check result: %v", err.Error())
		}

		return fmt.Errorf("Migration post-check failed: found %d not migrated versions and %d not migrated comparisons. ",
			len(postCheckResult.NotMigratedVersions), len(postCheckResult.NotMigratedComparisons))
	}

	return nil
}
