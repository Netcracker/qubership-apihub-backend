package stages

import (
	"fmt"
)

func (d OpsMigration) StageStarting() error {
	err := d.createTempTables()
	if err != nil {
		return fmt.Errorf("ops migration %s: failed to create temp tables: %w", d.ent.Id, err)
	}

	return nil
}

func (d OpsMigration) createTempTables() error {
	// create temporary tables required for suspicious builds analysis
	_, err := d.cp.GetConnection().Exec(`create schema if not exists migration;`)
	if err != nil {
		return err
	}
	_, err = d.cp.GetConnection().Exec(fmt.Sprintf(`create table migration."version_comparison_%s" as select * from version_comparison;`, d.ent.Id))
	if err != nil {
		return err
	}
	_, err = d.cp.GetConnection().Exec(fmt.Sprintf(`create table migration."operation_comparison_%s" as select * from operation_comparison;`, d.ent.Id))
	if err != nil {
		return err
	}
	_, err = d.cp.GetConnection().Exec(fmt.Sprintf(`create index "operation_comparison_%s_comparison_id_index" on migration."operation_comparison_%s" (comparison_id);`, d.ent.Id, d.ent.Id))
	if err != nil {
		return err
	}
	_, err = d.cp.GetConnection().Exec(fmt.Sprintf(`create table migration."expired_ts_operation_data_%s" (package_id varchar, version varchar, revision integer);`, d.ent.Id))
	if err != nil {
		return err
	}
	return nil
}
