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

package entity

import "github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"

type PackageGroupCountsEntity struct {
	Workspaces        int `pg:"workspaces"`
	DeletedWorkspaces int `pg:"deleted_workspaces"`
	Groups            int `pg:"groups"`
	DeletedGroups     int `pg:"deleted_groups"`
	Packages          int `pg:"packages"`
	DeletedPackages   int `pg:"deleted_packages"`
}

type RevisionsCountEntity struct {
	Revisions        int `pg:"revisions"`
	DeletedRevisions int `pg:"deleted_revisions"`
}

type BuildsCountEntity struct {
	BuildType        string `pg:"build_type"`
	NotStarted       int    `pg:"not_started"`
	Running          int    `pg:"running"`
	FailedLastWeek   int    `pg:"failed_last_week"`
	SucceedLastWeek  int    `pg:"succeed_last_week"`
	RestartsLastWeek int    `pg:"restarts_last_week"`
}

func (e *BuildsCountEntity) MakeBuildsCountView() view.BuildsCount {
	return view.BuildsCount{
		NotStarted:       e.NotStarted,
		Running:          e.Running,
		FailedLastWeek:   e.FailedLastWeek,
		SucceedLastWeek:  e.SucceedLastWeek,
		RestartsLastWeek: e.RestartsLastWeek,
	}
}

type TableSizeEntity struct {
	TableName      string  `pg:"table_name"`
	RowEstimate    float64 `pg:"row_estimate"`
	TotalSize      string  `pg:"total"`
	IndexSize      string  `pg:"index"`
	ToastSize      string  `pg:"toast"`
	TableSize      string  `pg:"table"`
	TotalSizeShare float64 `pg:"total_size_share"`
}

func (e *TableSizeEntity) MakeTableSizeInfoView() view.TableSizeInfo {
	return view.TableSizeInfo{
		TableName:      e.TableName,
		RowEstimate:    e.RowEstimate,
		TotalSize:      e.TotalSize,
		IndexSize:      e.IndexSize,
		ToastSize:      e.ToastSize,
		TableSize:      e.TableSize,
		TotalSizeShare: e.TotalSizeShare,
	}
}
