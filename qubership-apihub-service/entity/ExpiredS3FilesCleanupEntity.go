package entity

import "time"

type ExpiredS3FilesCleanupEntity struct {
	tableName struct{} `pg:"expired_s3_files_cleanup_run"`

	RunId        string     `pg:"run_id, pk, type:uuid"`
	StartedAt    time.Time  `pg:"started_at, type:timestamp without time zone"`
	FinishedAt   *time.Time `pg:"finished_at, type:timestamp without time zone"`
	Details      string     `pg:"details, type:varchar"`
	DeletedItems int        `pg:"deleted_items, type:integer"`
}
