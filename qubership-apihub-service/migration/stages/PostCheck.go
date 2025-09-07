package stages

import (
	"fmt"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/entity"

	"strings"
)

// StagePostCheck check that migration affected all required versions and comparisons!
func (d OpsMigration) StagePostCheck() error {
	// self-check

	// TODO: different logic for d.ent.IsRebuildChangelogOnly

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

	notMigratedVersionsQuery := fmt.Sprintf(`
    select * from published_version v left join build b
	                                            on v.package_id=b.package_id and v.version=(string_to_array(b.version, '@'))[1]
	                                                   and v.revision=(string_to_array(b.version, '@'))[2]::int
	                                                   and b.metadata->>'migration_id'='%s'
	where v.deleted_at is null and b.package_id is null %s %s`, d.ent.Id, wherePackageIn, whereVersionIn)

	var notMigratedVersions []entity.PublishedVersionEntity
	_, err := d.cp.GetConnection().Query(&notMigratedVersions, notMigratedVersionsQuery)
	if err != nil {
		return fmt.Errorf("failed to query not migrated versions: %v", err.Error())
	}

	// TODO: not migrated changelogs

	// TODO: report as errors

	return nil
}
