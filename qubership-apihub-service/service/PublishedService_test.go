package service

import (
	"testing"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/archive"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/entity"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/repository"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
)

func pv(version string, revision int, previousVersion string) entity.PackageVersionRevisionEntity {
	return entity.PackageVersionRevisionEntity{
		PublishedVersionEntity: entity.PublishedVersionEntity{
			Version:         version,
			Revision:        revision,
			PreviousVersion: previousVersion,
		},
	}
}

// Core logic of CheckPreviousVersionDependencyCycle is tested via detectPreviousVersionDependencyCycleWithCurrVersion
// (same package) to avoid a full PublishedRepository stub.
func TestCheckPreviousVersionDependencyCycle_graph(t *testing.T) {
	tests := []struct {
		name        string
		nodes       []entity.PackageVersionRevisionEntity
		version     string
		prevVersion string
		revision    int
		wantCycle   bool
	}{
		{
			name:        "linear chain, no cycle",
			nodes:       []entity.PackageVersionRevisionEntity{pv("0.9", 1, ""), pv("1.0", 1, "0.9")},
			version:     "2.0",
			prevVersion: "1.0",
			revision:    1,
			wantCycle:   false,
		},
		{
			name:        "empty history, first publish only simulated",
			nodes:       nil,
			version:     "1.0",
			prevVersion: "0.9",
			revision:    1,
			wantCycle:   false,
		},
		{
			name: "two revisions linked to the same previous version",
			nodes: []entity.PackageVersionRevisionEntity{
				pv("1.0", 1, ""),
				pv("2.0", 1, "1.0"),
				pv("2.0", 2, "1.0"),
			},
			version:     "3.0",
			prevVersion: "2.0",
			revision:    1,
			wantCycle:   false,
		},
		{
			name: "additional revision of a new version published",
			nodes: []entity.PackageVersionRevisionEntity{
				pv("0.9", 1, ""),
				pv("1.0", 1, "0.9"),
			},
			version:     "1.0",
			prevVersion: "0.9",
			revision:    2,
			wantCycle:   false,
		},
		{
			name: "old version publication that introduces cycle",
			nodes: []entity.PackageVersionRevisionEntity{
				pv("1", 1, ""),
				pv("2", 1, "1"),
				pv("3", 1, "2"),
			},
			version:     "2",
			prevVersion: "3",
			revision:    2,
			wantCycle:   true,
		},
		{
			name: "new correct publication with cycle in history",
			nodes: []entity.PackageVersionRevisionEntity{
				pv("1", 1, ""),
				pv("2", 1, ""),
				pv("2", 2, "3"),
				pv("2", 3, "1"),
				pv("3", 1, "2"),
			},
			version:     "2",
			prevVersion: "1",
			revision:    4,
			wantCycle:   false,
		},
		{
			name: "new correct publication with cycle in history 2",
			nodes: []entity.PackageVersionRevisionEntity{
				pv("1", 1, ""),
				pv("2", 1, ""),
				pv("2", 2, "3"),
				pv("3", 1, "2"),
			},
			version:     "2",
			prevVersion: "1",
			revision:    4,
			wantCycle:   false,
		},
		{
			name: "new correct publication with cycle in history - latest revisions only",
			nodes: []entity.PackageVersionRevisionEntity{
				pv("1", 1, ""),
				pv("2", 3, "1"),
				pv("3", 1, "2"),
			},
			version:     "2",
			prevVersion: "1",
			revision:    4,
			wantCycle:   false,
		},
		{
			name: "new incorrect publication",
			nodes: []entity.PackageVersionRevisionEntity{
				pv("1", 1, ""),
				pv("2", 3, "1"),
				pv("3", 1, "2"),
			},
			version:     "1",
			prevVersion: "3",
			revision:    2,
			wantCycle:   true,
		},
		{
			name: "new incorrect publication 2",
			nodes: []entity.PackageVersionRevisionEntity{
				pv("1", 1, ""),
				pv("2", 1, "1"),
				pv("3", 1, "2"),
			},
			version:     "2",
			prevVersion: "3",
			revision:    2,
			wantCycle:   true,
		},
		{
			name: "new incorrect publication 3",
			nodes: []entity.PackageVersionRevisionEntity{
				pv("1", 1, ""),
				pv("2", 1, "1"),
				pv("3", 1, "2"),
				pv("2", 2, "3"),
			},
			version:     "1",
			prevVersion: "3",
			revision:    2,
			wantCycle:   false,
		},
		{
			name: "longer revisions chain",
			nodes: []entity.PackageVersionRevisionEntity{
				pv("1", 1, ""),
				pv("1", 2, ""),
				pv("2", 1, "1"),
				pv("2", 2, "1"),
				pv("3", 1, "2"),
			},
			version:     "2",
			prevVersion: "1",
			revision:    3,
			wantCycle:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectPreviousVersionDependencyCycleWithCurrVersion(tt.nodes, tt.version, tt.prevVersion, tt.revision)
			if got != tt.wantCycle {
				t.Fatalf("detectPreviousVersionDependencyCycleWithCurrVersion(...) = %v, want %v for %s", got, tt.wantCycle, tt.name)
			}
		})
	}
}

type comparisonPublishedRepo struct {
	repository.PublishedRepository
	storedComparisons []entity.VersionComparisonEntity
}

func (r *comparisonPublishedRepo) GetVersionComparisonsByIds(comparisonIds []string) ([]entity.VersionComparisonEntity, error) {
	result := make([]entity.VersionComparisonEntity, 0, len(r.storedComparisons))
	for _, stored := range r.storedComparisons {
		for _, comparisonId := range comparisonIds {
			if stored.ComparisonId == comparisonId {
				result = append(result, stored)
			}
		}
	}
	return result, nil
}

func (r *comparisonPublishedRepo) GetVersionRefsV3(packageId string, version string, revision int) ([]entity.PublishedReferenceEntity, error) {
	if packageId == "dashboard2" && version == "v1" && revision == 1 {
		return []entity.PublishedReferenceEntity{{RefPackageId: "dashboard1", RefVersion: "v1", RefRevision: 1}}, nil
	}
	return nil, nil
}

func comparisonBuildReader(operationComparison view.VersionComparison, ddlComparison *view.DdlVersionComparison) *archive.BuildResultToEntitiesReader {
	buildResult := &archive.BuildResultArchive{
		PackageInfo: view.PackageInfoFile{
			PackageId:      "dashboard2",
			Version:        "v2",
			Revision:       1,
			BuilderVersion: "3.0.0",
		},
		PackageComparisons: view.PackageComparisonsFile{Comparisons: []view.VersionComparison{
			{PackageId: "dashboard2", Version: "v2", Revision: 1, PreviousVersionPackageId: "dashboard2", PreviousVersion: "v1", PreviousVersionRevision: 1},
			operationComparison,
		}},
	}
	if ddlComparison != nil {
		buildResult.PackageDdlComparisons.Comparisons = []view.DdlVersionComparison{*ddlComparison}
	}
	return archive.NewBuildResultToEntitiesReader(buildResult)
}

func readAndMergeVersionComparisons(t *testing.T, reader *archive.BuildResultToEntitiesReader, repo *comparisonPublishedRepo) ([]*entity.VersionComparisonEntity, []string) {
	t.Helper()
	resolver, err := reader.ResolveComparisonRefs([]*entity.PublishedReferenceEntity{{RefPackageId: "dashboard1", RefVersion: "v2", RefRevision: 1}}, repo)
	if err != nil {
		t.Fatalf("ResolveComparisonRefs() error = %v", err)
	}
	operationComparisons, _, _, err := reader.ReadOperationComparisonsToEntities(nil, nil, resolver)
	if err != nil {
		t.Fatalf("ReadOperationComparisonsToEntities() error = %v", err)
	}
	ddlComparisons, _, _, err := reader.ReadDdlContractComparisonsToEntities(nil, nil, resolver)
	if err != nil {
		t.Fatalf("ReadDdlContractComparisonsToEntities() error = %v", err)
	}
	return mergeVersionComparisons(operationComparisons, ddlComparisons, resolver), resolver.SkippedVersionComparisonIds()
}

func findVersionComparison(comparisons []*entity.VersionComparisonEntity, comparisonId string) *entity.VersionComparisonEntity {
	for _, comparison := range comparisons {
		if comparison.ComparisonId == comparisonId {
			return comparison
		}
	}
	return nil
}

func TestMergeVersionComparisonsMixedCache(t *testing.T) {
	dashboardComparisonId := view.MakeVersionComparisonId("dashboard1", "v2", 1, "dashboard1", "v1", 1)
	operationComparison := view.VersionComparison{
		PackageId:                "dashboard1",
		Version:                  "v2",
		Revision:                 1,
		PreviousVersionPackageId: "dashboard1",
		PreviousVersion:          "v1",
		PreviousVersionRevision:  1,
		OperationTypes:           []view.OperationType{{ApiType: "rest"}},
	}
	ddlComparison := view.DdlVersionComparison{
		PackageId:                "dashboard1",
		Version:                  "v2",
		Revision:                 1,
		PreviousVersionPackageId: "dashboard1",
		PreviousVersion:          "v1",
		PreviousVersionRevision:  1,
		ContractsChangesSummary: map[string]view.ContractTypeSummary{
			"ddl": {},
		},
	}

	t.Run("operation summary is preserved when ddl is rebuilt", func(t *testing.T) {
		storedOperationTypes := []view.OperationType{{ApiType: "stored-rest"}}
		reader := comparisonBuildReader(operationComparison, &ddlComparison)
		comparisons, skippedIds := readAndMergeVersionComparisons(t, reader, &comparisonPublishedRepo{
			storedComparisons: []entity.VersionComparisonEntity{{
				ComparisonId:   dashboardComparisonId,
				BuilderVersion: "3.0.0",
				OperationTypes: storedOperationTypes,
			}},
		})
		comparison := findVersionComparison(comparisons, dashboardComparisonId)
		if comparison == nil {
			t.Fatal("dashboard comparison parent was not produced")
		}
		if len(comparison.OperationTypes) != 1 || comparison.OperationTypes[0].ApiType != "stored-rest" {
			t.Errorf("operation types = %v, want stored operation summary", comparison.OperationTypes)
		}
		if len(comparison.ContractTypes) != 1 || comparison.ContractTypes[0].ContractType != "ddl" {
			t.Errorf("contract types = %v, want rebuilt DDL summary", comparison.ContractTypes)
		}
		if len(skippedIds) != 0 {
			t.Errorf("skipped comparison ids = %v, want none", skippedIds)
		}
	})

	t.Run("ddl summary is preserved when operation comparison is rebuilt", func(t *testing.T) {
		cachedDdlComparison := ddlComparison
		cachedDdlComparison.FromCache = true
		reader := comparisonBuildReader(operationComparison, &cachedDdlComparison)
		comparisons, skippedIds := readAndMergeVersionComparisons(t, reader, &comparisonPublishedRepo{
			storedComparisons: []entity.VersionComparisonEntity{{
				ComparisonId:   dashboardComparisonId,
				BuilderVersion: "3.0.0",
				ContractTypes:  []view.ContractType{{ContractType: "stored-ddl"}},
			}},
		})
		comparison := findVersionComparison(comparisons, dashboardComparisonId)
		if comparison == nil {
			t.Fatal("dashboard comparison parent was not produced")
		}
		if len(comparison.OperationTypes) != 1 || comparison.OperationTypes[0].ApiType != "rest" {
			t.Errorf("operation types = %v, want rebuilt operation summary", comparison.OperationTypes)
		}
		if len(comparison.ContractTypes) != 1 || comparison.ContractTypes[0].ContractType != "stored-ddl" {
			t.Errorf("contract types = %v, want stored DDL summary", comparison.ContractTypes)
		}
		if len(skippedIds) != 0 {
			t.Errorf("skipped comparison ids = %v, want none", skippedIds)
		}
	})

	t.Run("fully cached non-main parent is skipped", func(t *testing.T) {
		reader := comparisonBuildReader(operationComparison, &ddlComparison)
		comparisons, skippedIds := readAndMergeVersionComparisons(t, reader, &comparisonPublishedRepo{
			storedComparisons: []entity.VersionComparisonEntity{{
				ComparisonId:   dashboardComparisonId,
				BuilderVersion: "3.0.0",
				OperationTypes: []view.OperationType{{ApiType: "stored-rest"}},
				ContractTypes:  []view.ContractType{{ContractType: "stored-ddl"}},
			}},
		})
		if comparison := findVersionComparison(comparisons, dashboardComparisonId); comparison != nil {
			t.Errorf("fully cached dashboard parent = %v, want nil", comparison)
		}
		if len(skippedIds) != 1 || skippedIds[0] != dashboardComparisonId {
			t.Errorf("skipped comparison ids = %v, want %v", skippedIds, []string{dashboardComparisonId})
		}
	})

	t.Run("empty rebuilt ddl summary is not replaced with stored data", func(t *testing.T) {
		mainComparisonId := view.MakeVersionComparisonId("dashboard2", "v2", 1, "dashboard2", "v1", 1)
		mainDdlComparison := view.DdlVersionComparison{
			PackageId:                "dashboard2",
			Version:                  "v2",
			Revision:                 1,
			PreviousVersionPackageId: "dashboard2",
			PreviousVersion:          "v1",
			PreviousVersionRevision:  1,
			ContractsChangesSummary:  map[string]view.ContractTypeSummary{},
		}
		reader := comparisonBuildReader(operationComparison, &mainDdlComparison)
		comparisons, _ := readAndMergeVersionComparisons(t, reader, &comparisonPublishedRepo{
			storedComparisons: []entity.VersionComparisonEntity{{
				ComparisonId:   mainComparisonId,
				BuilderVersion: "3.0.0",
				ContractTypes:  []view.ContractType{{ContractType: "stored-ddl"}},
			}},
		})
		comparison := findVersionComparison(comparisons, mainComparisonId)
		if comparison == nil {
			t.Fatal("main comparison parent was not produced")
		}
		if comparison.ContractTypes != nil {
			t.Errorf("contract types = %v, want intentional nil summary from archive", comparison.ContractTypes)
		}
	})

	t.Run("summary from another builder version is not preserved", func(t *testing.T) {
		reader := comparisonBuildReader(operationComparison, nil)
		comparisons, _ := readAndMergeVersionComparisons(t, reader, &comparisonPublishedRepo{
			storedComparisons: []entity.VersionComparisonEntity{{
				ComparisonId:   dashboardComparisonId,
				BuilderVersion: "2.0.0",
				ContractTypes:  []view.ContractType{{ContractType: "stored-ddl"}},
			}},
		})
		comparison := findVersionComparison(comparisons, dashboardComparisonId)
		if comparison == nil {
			t.Fatal("dashboard comparison parent was not produced")
		}
		if comparison.ContractTypes != nil {
			t.Errorf("contract types = %v, want no summary from another builder version", comparison.ContractTypes)
		}
	})
}
