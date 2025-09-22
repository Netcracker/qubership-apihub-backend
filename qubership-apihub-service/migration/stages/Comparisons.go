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

	mView "github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/migration/view"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"

	"strings"
)

func (d OpsMigration) StageComparisonsOther() error {
	_, err := d.waitForBuilds(mView.MigrationStageComparisonsOther, 1) // for recovery
	if err != nil {
		return err
	}

	query := makeOtherComparisonsQuery(d.ent.PackageIds, d.ent.Versions, d.ent.Id)

	count, err := d.createComparisonBuilds(query, d.ent.Id)
	if err != nil {
		return fmt.Errorf("migration %s stage %s round %d: %w", d.ent.Id, mView.MigrationStageComparisonsOther, 1, err)
	}

	if count > 0 {
		_, err = d.waitForBuilds(mView.MigrationStageComparisonsOther, 1)
		if err != nil {
			return err
		}
	}

	return nil
}

func (d OpsMigration) StageComparisonsOnly() error {
	_, err := d.waitForBuilds(mView.MigrationStageComparisonsOnly, 1) // for recovery
	if err != nil {
		return err
	}

	query := makeComparisonsOnlyQuery(d.ent.PackageIds, d.ent.Versions)

	count, err := d.createComparisonBuilds(query, d.ent.Id)
	if err != nil {
		return fmt.Errorf("migration %s stage %s round %d: %w", d.ent.Id, mView.MigrationStageComparisonsOnly, 1, err)
	}

	if count > 0 {
		_, err = d.waitForBuilds(mView.MigrationStageComparisonsOnly, 1)
		if err != nil {
			return err
		}
	}

	return nil
}

func makeOtherComparisonsQuery(packageIds []string, versionsIn []string, migrationId string) string {
	var wherePackageIn string
	if len(packageIds) > 0 {
		wherePackageIn = " and vc.package_id in ("
		for i, pkg := range packageIds {
			if i > 0 {
				wherePackageIn += ","
			}
			wherePackageIn += fmt.Sprintf("'%s'", pkg)
		}
		wherePackageIn += ") "
	}

	var whereVersionIn string
	if len(versionsIn) > 0 {
		whereVersionIn = " and vc.version in ("
		for i, ver := range versionsIn {
			if i > 0 {
				whereVersionIn += ","
			}
			verSplit := strings.Split(ver, "@")
			whereVersionIn += fmt.Sprintf("'%s'", verSplit[0])
		}
		whereVersionIn += ") "
	}

	query :=
		fmt.Sprintf(
			`select vc.* from version_comparison vc
			inner join published_version pv1 on vc.package_id=pv1.package_id and vc.version=pv1.version and vc.revision=pv1.revision
			inner join published_version pv2 on vc.previous_package_id=pv2.package_id and vc.previous_version=pv2.version and vc.previous_revision=pv2.revision
			inner join package_group pg1 on vc.package_id=pg1.id
			inner join package_group pg2 on vc.previous_package_id=pg2.id
			where pv1.deleted_at is null and pv2.deleted_at is null and pg1.deleted_at is null and pg2.deleted_at is null
			/* at this stage we migrate only ad-hoc comparisons */
			and (vc.previous_package_id!=(CASE WHEN (pv1.previous_version_package_id IS NULL OR pv1.previous_version_package_id = '') THEN pv1.package_id ELSE pv1.previous_version_package_id END)
			or vc.previous_version!=coalesce(pv1.previous_version, ''))
			/* both versions should be migrated */
			and exists (select 1 from build b1 where b1.package_id = vc.package_id and b1.version like vc.version || '@%%' and b1.metadata->>'migration_id' = '%s' and b1.metadata->>'build_type' = 'build' and  b1.status='%s')
			and exists (select 1 from build b2 where b2.package_id = vc.previous_package_id and b2.version like vc.previous_version || '@%%' and b2.metadata->>'migration_id' = '%s' and b2.metadata->>'build_type' = 'build' and b2.status='%s') %s %s
        `, migrationId, view.StatusComplete, migrationId, view.StatusComplete, wherePackageIn, whereVersionIn)
	return query
}

func makeComparisonsOnlyQuery(packageIds []string, versionsIn []string) string {
	var wherePackageIn string
	if len(packageIds) > 0 {
		wherePackageIn = " and vc.package_id in ("
		for i, pkg := range packageIds {
			if i > 0 {
				wherePackageIn += ","
			}
			wherePackageIn += fmt.Sprintf("'%s'", pkg)
		}
		wherePackageIn += ") "
	}

	var whereVersionIn string
	if len(versionsIn) > 0 {
		whereVersionIn = " and vc.version in ("
		for i, ver := range versionsIn {
			if i > 0 {
				whereVersionIn += ","
			}
			verSplit := strings.Split(ver, "@")
			whereVersionIn += fmt.Sprintf("'%s'", verSplit[0])
		}
		whereVersionIn += ") "
	}

	query :=
		fmt.Sprintf(
			`select vc.* from version_comparison vc inner join published_version pv1 on
            vc.package_id=pv1.package_id
                and vc.version=pv1.version
                and vc.revision=pv1.revision
inner join published_version pv2 on vc.previous_package_id=pv2.package_id
and vc.previous_version=pv2.version
and vc.previous_revision=pv2.revision
         inner join package_group pg1 on vc.package_id=pg1.id
         inner join package_group pg2 on vc.previous_package_id=pg2.id
where
    pv1.deleted_at is null and pv2.deleted_at is null and pg1.deleted_at is null and pg2.deleted_at is null %s %s
        `, wherePackageIn, whereVersionIn)
	return query
}
