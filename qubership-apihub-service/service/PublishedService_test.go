package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/archive"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/exception"

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

func TestMergeVersionComparisons(t *testing.T) {
	mainId := view.MakeVersionComparisonId("d", "v2", 1, "d", "v1", 1)
	sharedId := view.MakeVersionComparisonId("p1", "v2", 1, "p1", "v1", 1)
	opOnlyId := view.MakeVersionComparisonId("p2", "v2", 1, "p2", "v1", 1)
	ddlOnlyId := view.MakeVersionComparisonId("p3", "v2", 1, "p3", "v1", 1)

	operationComparisons := []*entity.VersionComparisonEntity{
		{ComparisonId: mainId, OperationTypes: []view.OperationType{{ApiType: "rest"}}, Refs: []string{sharedId, opOnlyId}},
		{ComparisonId: sharedId, OperationTypes: []view.OperationType{{ApiType: "rest"}}},
		{ComparisonId: opOnlyId, OperationTypes: []view.OperationType{{ApiType: "rest"}}},
	}
	ddlComparisons := []*entity.VersionComparisonEntity{
		{ComparisonId: mainId, Refs: []string{sharedId, ddlOnlyId}},
		{ComparisonId: sharedId, ContractTypes: []view.ContractType{{ContractType: view.ContractTypeDdl}}},
		{ComparisonId: ddlOnlyId, ContractTypes: []view.ContractType{{ContractType: view.ContractTypeDdl}}},
	}

	merged := mergeVersionComparisons(operationComparisons, ddlComparisons)

	byId := make(map[string]*entity.VersionComparisonEntity, len(merged))
	for _, comparison := range merged {
		byId[comparison.ComparisonId] = comparison
	}
	if len(merged) != 4 {
		t.Fatalf("merged %d comparisons, want 4", len(merged))
	}
	if byId[sharedId].ContractTypes == nil {
		t.Errorf("shared comparison must carry the rebuilt DDL contract types")
	}
	if byId[opOnlyId].ContractTypes != nil {
		t.Errorf("operation-only comparison must not gain contract types from the merge; the DB layer is responsible for preserving the stored value")
	}
	if byId[ddlOnlyId].OperationTypes != nil {
		t.Errorf("ddl-only comparison must not gain operation types from the merge; the DB layer is responsible for preserving the stored value")
	}
	if byId[mainId].ContractTypes != nil {
		t.Errorf("main comparison has no DDL data, contract types must stay empty")
	}
	// The dashboard's main comparison references one package with only operation changes (opOnlyId)
	// and another with only DDL changes (ddlOnlyId); each reader only records refs for the
	// comparisons it produced, so the merge must union them or the DDL-only ref is dropped.
	wantRefs := map[string]bool{sharedId: true, opOnlyId: true, ddlOnlyId: true}
	if len(byId[mainId].Refs) != len(wantRefs) {
		t.Fatalf("main comparison refs = %v, want union of %v", byId[mainId].Refs, wantRefs)
	}
	for _, ref := range byId[mainId].Refs {
		if !wantRefs[ref] {
			t.Errorf("main comparison refs contains unexpected ref %s", ref)
		}
	}
}

// Only the DDL reader records errors found in DDL contracts. The merged row is the only one stored,
// so a changelog whose REST side is clean and whose DDL side has errors must still come out flagged.
func TestMergeVersionComparisonsUnionsHasErrors(t *testing.T) {
	sharedId := view.MakeVersionComparisonId("p1", "v2", 1, "p1", "v1", 1)
	ddlOnlyId := view.MakeVersionComparisonId("p2", "v2", 1, "p2", "v1", 1)

	errored := func(id string) *entity.VersionComparisonEntity {
		metadata := entity.Metadata{}
		metadata.SetHasErrors(true)
		return &entity.VersionComparisonEntity{ComparisonId: id, Metadata: metadata}
	}

	for _, tc := range []struct {
		name          string
		operationSide *entity.VersionComparisonEntity
		ddlSide       *entity.VersionComparisonEntity
		wantHasErrors bool
	}{
		{
			name:          "errors only on the ddl side",
			operationSide: &entity.VersionComparisonEntity{ComparisonId: sharedId, Metadata: entity.Metadata{}},
			ddlSide:       errored(sharedId),
			wantHasErrors: true,
		},
		{
			name:          "errors only on the operation side",
			operationSide: errored(sharedId),
			ddlSide:       &entity.VersionComparisonEntity{ComparisonId: sharedId, Metadata: entity.Metadata{}},
			wantHasErrors: true,
		},
		{
			name:          "errors on both sides",
			operationSide: errored(sharedId),
			ddlSide:       errored(sharedId),
			wantHasErrors: true,
		},
		{
			name:          "no errors on either side",
			operationSide: &entity.VersionComparisonEntity{ComparisonId: sharedId, Metadata: entity.Metadata{}},
			ddlSide:       &entity.VersionComparisonEntity{ComparisonId: sharedId, Metadata: entity.Metadata{}},
			wantHasErrors: false,
		},
		{
			// The readers always initialise the map, but a nil one must not panic the publish path.
			name:          "nil metadata on the operation side",
			operationSide: &entity.VersionComparisonEntity{ComparisonId: sharedId},
			ddlSide:       errored(sharedId),
			wantHasErrors: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			merged := mergeVersionComparisons(
				[]*entity.VersionComparisonEntity{tc.operationSide},
				[]*entity.VersionComparisonEntity{tc.ddlSide})
			if len(merged) != 1 {
				t.Fatalf("merged %d comparisons, want 1", len(merged))
			}
			if got := merged[0].Metadata.GetHasErrors(); got != tc.wantHasErrors {
				t.Errorf("merged has_errors = %v, want %v", got, tc.wantHasErrors)
			}
		})
	}

	t.Run("ddl-only comparison keeps its own errors", func(t *testing.T) {
		merged := mergeVersionComparisons(nil, []*entity.VersionComparisonEntity{errored(ddlOnlyId)})
		if len(merged) != 1 {
			t.Fatalf("merged %d comparisons, want 1", len(merged))
		}
		if !merged[0].Metadata.GetHasErrors() {
			t.Errorf("ddl-only comparison must keep the error flag it was read with")
		}
	})
}

type errorSummaryRepoStub struct {
	repository.PublishedRepository
	errorSummary         *entity.VersionErrorSummaryEntity
	erroredReferences    bool
	referencesLookupDone bool
}

func (s *errorSummaryRepoStub) GetVersionErrorSummary(context.Context, string, string, int) (*entity.VersionErrorSummaryEntity, error) {
	return s.errorSummary, nil
}

func (s *errorSummaryRepoStub) VersionHasErroredReferences(context.Context, string, string, int) (bool, error) {
	s.referencesLookupDone = true
	return s.erroredReferences, nil
}

func TestVersionHasAnyErrors(t *testing.T) {
	tests := []struct {
		name              string
		errorSummary      *entity.VersionErrorSummaryEntity
		erroredReferences bool
		expected          bool
	}{
		{
			name:         "version without errors",
			errorSummary: &entity.VersionErrorSummaryEntity{},
			expected:     false,
		},
		{
			name:         "errors in the version's own documents",
			errorSummary: &entity.VersionErrorSummaryEntity{HasErrors: true},
			expected:     true,
		},
		{
			// The version's documents are fine, but the changes it publishes cannot be trusted.
			name:         "errors in the changelog the version declares",
			errorSummary: &entity.VersionErrorSummaryEntity{ChangelogHasErrors: true},
			expected:     true,
		},
		{
			// A dashboard owns no documents, so its own flags say nothing about its references.
			name:              "dashboard referencing a version with errors",
			errorSummary:      &entity.VersionErrorSummaryEntity{},
			erroredReferences: true,
			expected:          true,
		},
		{
			// Nothing to judge: an absent version cannot block anything, and the caller reports it missing.
			name:         "version does not exist",
			errorSummary: nil,
			expected:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &errorSummaryRepoStub{errorSummary: tt.errorSummary, erroredReferences: tt.erroredReferences}
			hasErrors, err := VersionHasAnyErrors(context.Background(), repo, "QS.PKG", "2026.1", 3)
			if err != nil {
				t.Fatalf("expected the predicate to answer, but it failed: %v", err)
			}
			if hasErrors != tt.expected {
				t.Fatalf("expected hasErrors=%v, got %v", tt.expected, hasErrors)
			}
		})
	}
}

// The references lookup is the second query, so a version whose own flags already report errors must not pay for it.
func TestVersionHasAnyErrorsSkipsReferencesWhenVersionAlreadyHasErrors(t *testing.T) {
	repo := &errorSummaryRepoStub{errorSummary: &entity.VersionErrorSummaryEntity{HasErrors: true}}

	if _, err := VersionHasAnyErrors(context.Background(), repo, "QS.PKG", "2026.1", 3); err != nil {
		t.Fatalf("expected the predicate to answer, but it failed: %v", err)
	}
	if repo.referencesLookupDone {
		t.Fatal("expected the references lookup to be skipped for a version that already has errors")
	}
}

type failingErrorSummaryRepoStub struct {
	repository.PublishedRepository
}

var errErrorSummaryLookup = errors.New("version error summary lookup failed")

func (failingErrorSummaryRepoStub) GetVersionErrorSummary(context.Context, string, string, int) (*entity.VersionErrorSummaryEntity, error) {
	return nil, errErrorSummaryLookup
}

// A failed lookup must not read as "no errors": the refusals would silently let a version with errors through.
func TestVersionHasAnyErrorsPropagatesLookupFailure(t *testing.T) {
	_, err := VersionHasAnyErrors(context.Background(), failingErrorSummaryRepoStub{}, "QS.PKG", "2026.1", 3)
	if !errors.Is(err, errErrorSummaryLookup) {
		t.Fatalf("expected the lookup failure to propagate, got %v", err)
	}
}

type dependentVersionsRepoStub struct {
	repository.PublishedRepository
	dependents []entity.PublishedVersionEntity
	err        error
}

func (s dependentVersionsRepoStub) GetVersionsByPreviousVersion(context.Context, string, string) ([]entity.PublishedVersionEntity, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.dependents, nil
}

type publisherAccessRoleServiceStub struct {
	RoleService
	accessible  []entity.PublishedVersionKeyEntity
	hidden      int
	err         error
	publisherId string
}

func (s *publisherAccessRoleServiceStub) FilterVersionsByPublisherReadAccess(_ context.Context, publisherId string, _ []entity.PublishedVersionKeyEntity) ([]entity.PublishedVersionKeyEntity, int, error) {
	s.publisherId = publisherId
	if s.err != nil {
		return nil, 0, s.err
	}
	return s.accessible, s.hidden, nil
}

func dependentVersion(packageId string, version string, revision int) entity.PublishedVersionEntity {
	return entity.PublishedVersionEntity{PackageId: packageId, Version: version, Revision: revision}
}

func dependentKey(packageId string, version string, revision int) entity.PublishedVersionKeyEntity {
	return entity.PublishedVersionKeyEntity{PackageId: packageId, Version: version, Revision: revision}
}

const testPublisherId = "publisher-id"

func erroredBuildArchive(hasErrors bool) *archive.BuildResultArchive {
	return &archive.BuildResultArchive{
		PackageInfo: view.PackageInfoFile{PackageId: "QS.PKG", Version: "2026.1", CreatedBy: testPublisherId, HasErrors: hasErrors},
	}
}

func TestCheckNoDependentVersionsForErroredVersion(t *testing.T) {
	tests := []struct {
		name            string
		hasErrors       bool
		dependents      []entity.PublishedVersionEntity
		accessible      []entity.PublishedVersionKeyEntity
		hidden          int
		expectedListing string
	}{
		{
			// Nothing to protect: the dependents' changelogs stay trustworthy.
			name:       "build without errors publishes",
			hasErrors:  false,
			dependents: []entity.PublishedVersionEntity{dependentVersion("QS.DEP", "1.0", 1)},
		},
		{
			name:      "errored build with no dependents publishes",
			hasErrors: true,
		},
		{
			name:            "every dependent is readable",
			hasErrors:       true,
			dependents:      []entity.PublishedVersionEntity{dependentVersion("QS.DEP", "1.0", 1), dependentVersion("QS.OTHER", "2.0", 3)},
			accessible:      []entity.PublishedVersionKeyEntity{dependentKey("QS.DEP", "1.0", 1), dependentKey("QS.OTHER", "2.0", 3)},
			expectedListing: "QS.DEP|1.0@1, QS.OTHER|2.0@3",
		},
		{
			name:            "some dependents are hidden",
			hasErrors:       true,
			dependents:      []entity.PublishedVersionEntity{dependentVersion("QS.DEP", "1.0", 1), dependentVersion("QS.SECRET", "2.0", 3)},
			accessible:      []entity.PublishedVersionKeyEntity{dependentKey("QS.DEP", "1.0", 1)},
			hidden:          1,
			expectedListing: "QS.DEP|1.0@1, and 1 more package version you cannot access (contact system administrator)",
		},
		{
			// The publisher still learns why the publication was refused, without learning what they may not see.
			name:            "no dependent is readable",
			hasErrors:       true,
			dependents:      []entity.PublishedVersionEntity{dependentVersion("QS.SECRET", "2.0", 3), dependentVersion("QS.OTHER", "1.0", 1)},
			hidden:          2,
			expectedListing: "2 package versions you cannot access (contact system administrator)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			roleService := &publisherAccessRoleServiceStub{accessible: tt.accessible, hidden: tt.hidden}
			service := publishedServiceImpl{
				publishedRepo: dependentVersionsRepoStub{dependents: tt.dependents},
				roleService:   roleService,
			}

			err := service.checkNoDependentVersionsForErroredVersion(context.Background(), erroredBuildArchive(tt.hasErrors))

			if tt.expectedListing == "" {
				if err != nil {
					t.Fatalf("expected the publication to be allowed, got %v", err)
				}
				return
			}

			customErr, ok := err.(*exception.CustomError)
			if !ok {
				t.Fatalf("expected a CustomError, got %T: %v", err, err)
			}
			if customErr.Code != exception.VersionHasErrors {
				t.Fatalf("expected code %v, got %v", exception.VersionHasErrors, customErr.Code)
			}
			if customErr.Params["dependentVersions"] != tt.expectedListing {
				t.Fatalf("expected dependentVersions %q, got %q", tt.expectedListing, customErr.Params["dependentVersions"])
			}
			if roleService.publisherId != testPublisherId {
				t.Fatalf("expected the filter to run for the publisher, got %q", roleService.publisherId)
			}
		})
	}
}

var errDependentsLookup = errors.New("dependent versions lookup failed")

// A failed lookup must not read as "no dependents": the publication would go through and break their changelogs.
func TestCheckNoDependentVersionsForErroredVersionPropagatesLookupFailure(t *testing.T) {
	service := publishedServiceImpl{
		publishedRepo: dependentVersionsRepoStub{err: errDependentsLookup},
		roleService:   &publisherAccessRoleServiceStub{},
	}

	err := service.checkNoDependentVersionsForErroredVersion(context.Background(), erroredBuildArchive(true))
	if !errors.Is(err, errDependentsLookup) {
		t.Fatalf("expected the lookup failure to propagate, got %v", err)
	}
}

var errReadAccessFilter = errors.New("read access filtering failed")

// The same reasoning: a filtering failure must refuse the publication, not allow it.
func TestCheckNoDependentVersionsForErroredVersionPropagatesFilterFailure(t *testing.T) {
	service := publishedServiceImpl{
		publishedRepo: dependentVersionsRepoStub{dependents: []entity.PublishedVersionEntity{dependentVersion("QS.DEP", "1.0", 1)}},
		roleService:   &publisherAccessRoleServiceStub{err: errReadAccessFilter},
	}

	err := service.checkNoDependentVersionsForErroredVersion(context.Background(), erroredBuildArchive(true))
	if !errors.Is(err, errReadAccessFilter) {
		t.Fatalf("expected the filtering failure to propagate, got %v", err)
	}
}
