package stages

import (
	"fmt"
	mView "github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/migration/view"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"

	"strings"
)

func (d OpsMigration) StageDependentVersionsLastRevs() error {
	round := 1

	_, err := d.waitForBuilds(mView.MigrationStageDependentVersionsLastRevs, round) // for recovery, but round number is not recovered since it's not significant in the whole procedure
	if err != nil {
		return err
	}

	count := 1
	for count > 0 {
		query := makeDependentVersionsQuery(d.ent.PackageIds, d.ent.Versions, d.ent.Id, true)

		count, err = d.createBuilds(query, d.ent.Id)
		if err != nil {
			return fmt.Errorf("migration %s stage %s round %d: %w", d.ent.Id, mView.MigrationStageDependentVersionsLastRevs, round, err)
		}

		if count > 0 {
			_, err = d.waitForBuilds(mView.MigrationStageDependentVersionsLastRevs, round)
		}
		round += 1
	}

	return nil
}

// TODO: collapse with StageDependentVersionsLastRevs
func (d OpsMigration) StageDependentVersionsOldRevs() error {
	round := 1

	_, err := d.waitForBuilds(mView.MigrationStageDependentVersionsOldRevs, round) // for recovery, but round number is not recovered since it's not significant in the whole procedure
	if err != nil {
		return err
	}

	count := 1
	for count > 0 {
		query := makeDependentVersionsQuery(d.ent.PackageIds, d.ent.Versions, d.ent.Id, false)

		count, err = d.createBuilds(query, d.ent.Id)
		if err != nil {
			return fmt.Errorf("migration %s stage %s round %d: %w", d.ent.Id, mView.MigrationStageDependentVersionsOldRevs, round, err)
		}

		if count > 0 {
			_, err = d.waitForBuilds(mView.MigrationStageDependentVersionsOldRevs, round)
		}
		round += 1
	}

	return nil
}

func makeDependentVersionsQuery(packageIds []string, versionsIn []string, migrationId string, isLatest bool) string {

	var wherePackageIn string
	if len(packageIds) > 0 {
		wherePackageIn = " and package_id in ("
		for i, pkg := range packageIds {
			if i > 0 {
				wherePackageIn += ","
			}
			wherePackageIn += fmt.Sprintf("'%s'", pkg) // TODO: SQL injection is possible here
		}
		wherePackageIn += ") "
	}

	var whereVersionIn string
	if len(versionsIn) > 0 {
		whereVersionIn = " and version in ("
		for i, ver := range versionsIn {
			if i > 0 {
				whereVersionIn += ","
			}
			verSplit := strings.Split(ver, "@")
			whereVersionIn += fmt.Sprintf("'%s'", verSplit[0]) // TODO: SQL injection is possible here
		}
		whereVersionIn += ") "
	}

	// TODO: replace with query impl: 'len(packageIds)=0 or package_id in (?)'

	query := `
	with maxrev as (
		select package_id, version, max(revision) as revision
			from published_version where deleted_at is null `

	if wherePackageIn != "" {
		query += wherePackageIn
	}
	if whereVersionIn != "" {
		query += whereVersionIn
	}

	var maxrevQueryOperator string
	if isLatest {
		maxrevQueryOperator = "=" // join on latest revision
	} else {
		maxrevQueryOperator = "!=" // join on NOT latest revision
	}

	query +=
		fmt.Sprintf(
			` group by package_id, version
			)
		select pv.* from
			published_version pv
				inner join maxrev
						   on pv.package_id = maxrev.package_id
							   and pv.version = maxrev.version
							   and pv.revision %s maxrev.revision
							   and pv.deleted_at is null
				inner join package_group pkg on pv.package_id = pkg.id
		where
			/*previous version is not empty, i.e. version have depend*/
			pv.previous_version is not null and
			/* previous version is migrated successfully(!), assuming that we're working with the last revision which should be already migrated */
			exists (select 1 from build b
					  where
						  b.package_id=(CASE WHEN (pv.previous_version_package_id IS NULL OR pv.previous_version_package_id = '') THEN pv.package_id ELSE pv.previous_version_package_id END)
						and b.version like pv.previous_version || '@%%' /* simple '=' will not work due to "v@r" format in build's version */
						and b.metadata->>'migration_id' = '%s'
						and b.status='%s'
			) and
			/*version is not migrated yet*/
			not exists(
			select 1 from build b
			where (string_to_array(b.version, '@'))[1] = pv.version /* simple = will not work due to "v@r" format in build */
			  and b.package_id = pv.package_id
			  and (string_to_array(b.version, '@'))[2]::int = pv.revision
			  and b.metadata->>'build_type' = 'build'
			  and b.metadata->>'migration_id' = '%s'
		) and pkg.deleted_at is null
		order by pv.published_at asc, pv.package_id asc, pv.version asc, pv.revision asc
	`, maxrevQueryOperator, migrationId, view.StatusComplete, migrationId)
	return query
}
