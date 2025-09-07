package stages

import (
	"fmt"
	mView "github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/migration/view"

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

	// TODO: skip changelogs for deleted versions

	// TODO: need to check that both version are already migrated
	query :=
		fmt.Sprintf(
			`select vc.* from version_comparison vc inner join published_version pv on
            vc.package_id=pv.package_id and vc.version=pv.version and vc.revision=pv.revision where
			vc.previous_package_id!=(CASE WHEN (pv.previous_version_package_id IS NULL OR pv.previous_version_package_id = '') THEN pv.package_id ELSE pv.previous_version_package_id END)
			and vc.previous_version!=coalesce(pv.previous_version, '') %s %s
        `, wherePackageIn, whereVersionIn)
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
			`sselect vc.* from version_comparison vc inner join published_version pv1 on
            vc.package_id=pv1.package_id
                and vc.version=pv1.version
                and vc.revision=pv1.revision
inner join published_version pv2 on vc.previous_package_id=pv2.package_id
and vc.previous_version=pv2.version
and vc.previous_revision=pv2.revision
         inner join package_group pg1 on vc.package_id=pg1.id
         inner join package_group pg2 on vc.package_id=pg2.id
where
    pv1.deleted_at is null and pv2.deleted_at is null and pg1.deleted_at is null and pg2.deleted_at is null %s %s
        `, wherePackageIn, whereVersionIn)
	return query
}
