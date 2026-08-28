package service

import (
	"context"
	"testing"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/entity"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/repository"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
)

const (
	listPackageId = "QS.DASH"
	listVersion   = "2026.1"
	listRevision  = 2
)

type versionsListRepoStub struct {
	repository.PublishedRepository
	kind            string
	previousVersion string
	errorSummary    entity.VersionErrorSummaryEntity
	summaryQueried  bool
}

func (s *versionsListRepoStub) GetPackage(context.Context, string) (*entity.PackageEntity, error) {
	return &entity.PackageEntity{Id: listPackageId, Kind: s.kind}, nil
}

func (s *versionsListRepoStub) GetReadonlyPackageVersionsWithLimit(context.Context, entity.PublishedVersionSearchQueryEntity, bool, bool) ([]entity.PackageVersionRevisionEntity, error) {
	return []entity.PackageVersionRevisionEntity{{
		PublishedVersionEntity: entity.PublishedVersionEntity{
			PackageId:       listPackageId,
			Version:         listVersion,
			Revision:        listRevision,
			Status:          string(view.Draft),
			PreviousVersion: s.previousVersion,
		},
	}}, nil
}

func (s *versionsListRepoStub) GetVersionsErrorSummary(_ context.Context, versionKeys []entity.PublishedVersionKeyEntity, _ bool) (map[entity.PublishedVersionKeyEntity]entity.VersionErrorSummaryEntity, error) {
	s.summaryQueried = true
	result := make(map[entity.PublishedVersionKeyEntity]entity.VersionErrorSummaryEntity, len(versionKeys))
	for _, key := range versionKeys {
		result[key] = s.errorSummary
	}
	return result, nil
}

// The versions list reports the same two flags as the version content endpoint, and reports them
// separately. A fact that belongs to neither - a referenced version's own unreliable changelog - must move
// neither flag, even though it does block the reference from being added.
func TestGetPackageVersionsViewSplitsErrorFlags(t *testing.T) {
	tests := []struct {
		name              string
		kind              string
		errorSummary      entity.VersionErrorSummaryEntity
		expectedContent   bool
		expectedChangelog bool
	}{
		{
			name:         "version without errors",
			kind:         entity.KIND_PACKAGE,
			errorSummary: entity.VersionErrorSummaryEntity{},
		},
		{
			name:            "the version's own documents failed",
			kind:            entity.KIND_PACKAGE,
			errorSummary:    entity.VersionErrorSummaryEntity{HasErrors: true},
			expectedContent: true,
		},
		{
			// A package version, not a dashboard: the changelog flag is reported for every kind.
			name:              "the version's own changelog failed",
			kind:              entity.KIND_PACKAGE,
			errorSummary:      entity.VersionErrorSummaryEntity{ChangelogHasErrors: true},
			expectedChangelog: true,
		},
		{
			name:            "a referenced version has errored documents",
			kind:            entity.KIND_DASHBOARD,
			errorSummary:    entity.VersionErrorSummaryEntity{ReferencedVersionHasErrors: true},
			expectedContent: true,
		},
		{
			name:              "a reference comparison of this changelog failed",
			kind:              entity.KIND_DASHBOARD,
			errorSummary:      entity.VersionErrorSummaryEntity{ComparisonRefsHaveErrors: true},
			expectedChangelog: true,
		},
		{
			name:         "a referenced version's own changelog is unreliable",
			kind:         entity.KIND_DASHBOARD,
			errorSummary: entity.VersionErrorSummaryEntity{ReferencedVersionChangelogHasErrors: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &versionsListRepoStub{kind: tt.kind, previousVersion: "2025.4", errorSummary: tt.errorSummary}
			service := versionServiceImpl{publishedRepo: repo}

			versions, err := service.GetPackageVersionsView(context.Background(), view.VersionListReq{
				PackageId: listPackageId,
				SortBy:    view.VersionSortByVersion,
				SortOrder: view.VersionSortOrderAsc,
			}, false)
			if err != nil {
				t.Fatalf("expected the versions to be listed, got %v", err)
			}
			if len(versions.Versions) != 1 {
				t.Fatalf("listed %d versions, want 1", len(versions.Versions))
			}
			if versions.Versions[0].HasErrors != tt.expectedContent {
				t.Errorf("hasErrors = %v, want %v", versions.Versions[0].HasErrors, tt.expectedContent)
			}
			if versions.Versions[0].ChangelogHasErrors == nil {
				t.Fatal("expected changelogHasErrors to be reported for a version that declares a previous version")
			}
			if *versions.Versions[0].ChangelogHasErrors != tt.expectedChangelog {
				t.Errorf("changelogHasErrors = %v, want %v", *versions.Versions[0].ChangelogHasErrors, tt.expectedChangelog)
			}
		})
	}
}

// "no previous version" and "the changelog is sound" are different statements, so a version with no
// baseline reports no changelog flag at all rather than a misleading false.
func TestGetPackageVersionsViewOmitsChangelogFlagWithoutABaseline(t *testing.T) {
	repo := &versionsListRepoStub{kind: entity.KIND_PACKAGE}
	service := versionServiceImpl{publishedRepo: repo}

	versions, err := service.GetPackageVersionsView(context.Background(), view.VersionListReq{
		PackageId: listPackageId,
		SortBy:    view.VersionSortByVersion,
		SortOrder: view.VersionSortOrderAsc,
	}, false)
	if err != nil {
		t.Fatalf("expected the versions to be listed, got %v", err)
	}
	if versions.Versions[0].ChangelogHasErrors != nil {
		t.Fatalf("expected no changelog flag, got %v", *versions.Versions[0].ChangelogHasErrors)
	}
}

// A package owns no references, but its own changelog can still be unreliable, so the flags are calculated
// for every kind rather than for dashboards only.
func TestGetPackageVersionsViewQueriesTheSummaryForPackagesToo(t *testing.T) {
	repo := &versionsListRepoStub{kind: entity.KIND_PACKAGE}
	service := versionServiceImpl{publishedRepo: repo}

	if _, err := service.GetPackageVersionsView(context.Background(), view.VersionListReq{
		PackageId: listPackageId,
		SortBy:    view.VersionSortByVersion,
		SortOrder: view.VersionSortOrderAsc,
	}, false); err != nil {
		t.Fatalf("expected the versions to be listed, got %v", err)
	}
	if !repo.summaryQueried {
		t.Fatal("expected the error summary to be queried for a package as well as for a dashboard")
	}
}
