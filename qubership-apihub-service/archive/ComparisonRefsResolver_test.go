package archive

import (
	"slices"
	"testing"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/entity"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
)

func vk(packageId, version string, revision int) VersionKey {
	return VersionKey{PackageId: packageId, Version: version, Revision: revision}
}

func comparisonId(current VersionKey, previous VersionKey) string {
	return view.MakeVersionComparisonId(current.PackageId, current.Version, current.Revision,
		previous.PackageId, previous.Version, previous.Revision)
}

func descendantSet(keys ...VersionKey) map[VersionKey]struct{} {
	result := make(map[VersionKey]struct{}, len(keys))
	for _, key := range keys {
		result[key] = struct{}{}
	}
	return result
}

func TestCollectNestedComparisonIds(t *testing.T) {
	parent := ComparisonRefCandidate{
		ComparisonId: "parent",
		Current:      vk("dashboard", "v2", 1),
		Previous:     vk("dashboard", "v1", 1),
	}
	changed := ComparisonRefCandidate{
		ComparisonId: "changed",
		Current:      vk("p1", "v2", 1),
		Previous:     vk("p1", "v1", 1),
	}
	added := ComparisonRefCandidate{
		ComparisonId: "added",
		Current:      vk("p2", "v1", 1),
	}
	removed := ComparisonRefCandidate{
		ComparisonId: "removed",
		Previous:     vk("p3", "v1", 1),
	}
	foreign := ComparisonRefCandidate{
		ComparisonId: "foreign",
		Current:      vk("other", "v1", 1),
		Previous:     vk("other", "v0", 1),
	}
	empty := ComparisonRefCandidate{ComparisonId: "empty"}
	candidates := []ComparisonRefCandidate{parent, changed, added, removed, foreign, empty}

	tests := []struct {
		name                string
		currentDescendants  map[VersionKey]struct{}
		previousDescendants map[VersionKey]struct{}
		want                []string
	}{
		{
			name:                "changed, added and removed refs are nested",
			currentDescendants:  descendantSet(vk("p1", "v2", 1), vk("p2", "v1", 1)),
			previousDescendants: descendantSet(vk("p1", "v1", 1), vk("p3", "v1", 1)),
			want:                []string{"changed", "added", "removed"},
		},
		{
			name:                "candidate with a non-referenced current side is not nested",
			currentDescendants:  descendantSet(vk("p2", "v1", 1)),
			previousDescendants: descendantSet(vk("p1", "v1", 1)),
			want:                []string{"added"},
		},
		{
			name: "one-sided candidates whose package changed on the opposite side are not nested",
			// p2 already existed on the previous side and p3 still exists on the current side,
			// so the parent changed those packages rather than adding or removing them
			currentDescendants:  descendantSet(vk("p3", "v2", 1)),
			previousDescendants: descendantSet(vk("p2", "v0", 1)),
			want:                nil,
		},
		{
			name:                "no descendants means no nested comparisons",
			currentDescendants:  descendantSet(),
			previousDescendants: descendantSet(),
			want:                nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CollectNestedComparisonIds(parent.ComparisonId, candidates, tt.currentDescendants, tt.previousDescendants)
			if !slices.Equal(got, tt.want) {
				t.Errorf("CollectNestedComparisonIds() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRefTreeDescendantsAreTransitive(t *testing.T) {
	root := vk("dashboard", "v1", 1)
	tree := newRefTree(root)
	// dashboard -> inner dashboard -> leaf package (parent link present),
	// plus a direct leaf without a parent link (defaults to the root)
	tree.addReference(entity.PublishedReferenceEntity{
		RefPackageId: "inner", RefVersion: "v1", RefRevision: 1,
	})
	tree.addReference(entity.PublishedReferenceEntity{
		RefPackageId: "leaf", RefVersion: "v1", RefRevision: 1,
		ParentRefPackageId: "inner", ParentRefVersion: "v1", ParentRefRevision: 1,
	})
	tree.addReference(entity.PublishedReferenceEntity{
		RefPackageId: "direct", RefVersion: "v1", RefRevision: 1,
	})

	rootDescendants := tree.descendants(root)
	for _, expected := range []VersionKey{vk("inner", "v1", 1), vk("leaf", "v1", 1), vk("direct", "v1", 1)} {
		if _, ok := rootDescendants[expected]; !ok {
			t.Errorf("root descendants are missing %v", expected)
		}
	}

	innerDescendants := tree.descendants(vk("inner", "v1", 1))
	if _, ok := innerDescendants[vk("leaf", "v1", 1)]; !ok {
		t.Errorf("inner descendants are missing the leaf")
	}
	if _, ok := innerDescendants[vk("direct", "v1", 1)]; ok {
		t.Errorf("inner descendants must not contain the root's direct reference")
	}
}

type fakeRefsStore struct {
	storedComparisons []entity.VersionComparisonEntity
	refsByVersion     map[VersionKey][]entity.PublishedReferenceEntity
	refsRequests      []VersionKey
}

func (s *fakeRefsStore) GetVersionComparisonsByIds(comparisonIds []string) ([]entity.VersionComparisonEntity, error) {
	result := make([]entity.VersionComparisonEntity, 0)
	for _, stored := range s.storedComparisons {
		if slices.Contains(comparisonIds, stored.ComparisonId) {
			result = append(result, stored)
		}
	}
	return result, nil
}

func (s *fakeRefsStore) GetVersionRefsV3(packageId string, version string, revision int) ([]entity.PublishedReferenceEntity, error) {
	key := vk(packageId, version, revision)
	s.refsRequests = append(s.refsRequests, key)
	return s.refsByVersion[key], nil
}

func dashboardPublishReader() *BuildResultToEntitiesReader {
	// dashboard d v2 (previous v1) referencing p1 and p2; p1 changed operations, p2 changed DDL
	return &BuildResultToEntitiesReader{
		BuildResultArchive: &BuildResultArchive{
			PackageInfo: view.PackageInfoFile{
				PackageId:               "d",
				Version:                 "v2",
				Revision:                1,
				PreviousVersion:         "v1",
				PreviousVersionRevision: 1,
				BuilderVersion:          "builder-1",
			},
			PackageComparisons: view.PackageComparisonsFile{
				Comparisons: []view.VersionComparison{
					{
						PackageId: "d", Version: "v2", Revision: 1,
						PreviousVersionPackageId: "d", PreviousVersion: "v1", PreviousVersionRevision: 1,
					},
					{
						PackageId: "p1", Version: "v2", Revision: 1,
						PreviousVersionPackageId: "p1", PreviousVersion: "v1", PreviousVersionRevision: 1,
					},
				},
			},
			PackageDdlComparisons: view.PackageDdlComparisonsFile{
				Comparisons: []view.DdlVersionComparison{
					{
						PackageId: "p2", Version: "v2", Revision: 1,
						PreviousVersionPackageId: "p2", PreviousVersion: "v1", PreviousVersionRevision: 1,
					},
				},
			},
		},
	}
}

func TestResolveComparisonRefs_dashboardPublish(t *testing.T) {
	reader := dashboardPublishReader()
	mainId := comparisonId(vk("d", "v2", 1), vk("d", "v1", 1))
	p1Id := comparisonId(vk("p1", "v2", 1), vk("p1", "v1", 1))
	p2Id := comparisonId(vk("p2", "v2", 1), vk("p2", "v1", 1))

	currentRefs := []*entity.PublishedReferenceEntity{
		{PackageId: "d", Version: "v2", Revision: 1, RefPackageId: "p1", RefVersion: "v2", RefRevision: 1},
		{PackageId: "d", Version: "v2", Revision: 1, RefPackageId: "p2", RefVersion: "v2", RefRevision: 1},
	}
	store := &fakeRefsStore{
		refsByVersion: map[VersionKey][]entity.PublishedReferenceEntity{
			vk("d", "v1", 1): {
				{PackageId: "d", Version: "v1", Revision: 1, RefPackageId: "p1", RefVersion: "v1", RefRevision: 1},
				{PackageId: "d", Version: "v1", Revision: 1, RefPackageId: "p2", RefVersion: "v1", RefRevision: 1},
			},
		},
	}

	resolver, err := reader.ResolveComparisonRefs(currentRefs, store)
	if err != nil {
		t.Fatalf("ResolveComparisonRefs() error: %v", err)
	}

	mainRefs := resolver.Refs(mainId)
	if !slices.Contains(mainRefs, p1Id) || !slices.Contains(mainRefs, p2Id) || len(mainRefs) != 2 {
		t.Errorf("main comparison refs = %v, want both nested comparison ids", mainRefs)
	}
	if !slices.Equal(resolver.OperationComparisonIdsToRebuild(), []string{mainId, p1Id}) {
		t.Errorf("operation rebuild ids = %v, want [main, p1]", resolver.OperationComparisonIdsToRebuild())
	}
	if !slices.Equal(resolver.DdlComparisonIdsToRebuild(), []string{p2Id}) {
		t.Errorf("ddl rebuild ids = %v, want [p2]", resolver.DdlComparisonIdsToRebuild())
	}
	if len(resolver.SkippedVersionComparisonIds()) != 0 {
		t.Errorf("skipped ids = %v, want none", resolver.SkippedVersionComparisonIds())
	}
	// the current side's references were passed in, so only the previous side hits the store
	if !slices.Equal(store.refsRequests, []VersionKey{vk("d", "v1", 1)}) {
		t.Errorf("refs requests = %v, want only the previous dashboard version", store.refsRequests)
	}
}

func TestResolveComparisonRefs_cachedComparisons(t *testing.T) {
	reader := dashboardPublishReader()
	reader.PackageComparisons.Comparisons[1].FromCache = true
	reader.PackageDdlComparisons.Comparisons[0].FromCache = true
	mainId := comparisonId(vk("d", "v2", 1), vk("d", "v1", 1))
	p1Id := comparisonId(vk("p1", "v2", 1), vk("p1", "v1", 1))
	p2Id := comparisonId(vk("p2", "v2", 1), vk("p2", "v1", 1))

	store := &fakeRefsStore{
		storedComparisons: []entity.VersionComparisonEntity{
			{ComparisonId: p1Id, BuilderVersion: "builder-1", OperationTypes: []view.OperationType{{ApiType: "rest"}}},
			{ComparisonId: p2Id, BuilderVersion: "builder-1", ContractTypes: []view.ContractType{{ContractType: view.ContractTypeDdl}}},
		},
	}

	resolver, err := reader.ResolveComparisonRefs([]*entity.PublishedReferenceEntity{}, store)
	if err != nil {
		t.Fatalf("ResolveComparisonRefs() error: %v", err)
	}

	if !resolver.IsOperationComparisonFromCache(p1Id) {
		t.Errorf("p1 operation comparison must be from cache")
	}
	if !resolver.IsDdlComparisonFromCache(p2Id) {
		t.Errorf("p2 ddl comparison must be from cache")
	}
	if resolver.IsOperationComparisonFromCache(mainId) {
		t.Errorf("main comparison must never be from cache")
	}
	skipped := resolver.SkippedVersionComparisonIds()
	if !slices.Contains(skipped, p1Id) || !slices.Contains(skipped, p2Id) || slices.Contains(skipped, mainId) {
		t.Errorf("skipped ids = %v, want the two fully cached refs", skipped)
	}
	if stored, ok := resolver.StoredComparison(p2Id); !ok || stored.ContractTypes == nil {
		t.Errorf("stored comparison for p2 must be available with contract types")
	}
}

func TestResolveComparisonRefs_cachedComparisonWithoutStoredDataFails(t *testing.T) {
	reader := dashboardPublishReader()
	reader.PackageComparisons.Comparisons[1].FromCache = true

	_, err := reader.ResolveComparisonRefs([]*entity.PublishedReferenceEntity{}, &fakeRefsStore{})
	if err == nil {
		t.Fatalf("ResolveComparisonRefs() must fail for a fromCache comparison without stored data")
	}
}

func TestResolveComparisonRefs_staleBuilderVersionIsNotCache(t *testing.T) {
	reader := dashboardPublishReader()
	p1Id := comparisonId(vk("p1", "v2", 1), vk("p1", "v1", 1))

	store := &fakeRefsStore{
		storedComparisons: []entity.VersionComparisonEntity{
			{ComparisonId: p1Id, BuilderVersion: "older-builder", OperationTypes: []view.OperationType{{ApiType: "rest"}}},
		},
	}
	resolver, err := reader.ResolveComparisonRefs([]*entity.PublishedReferenceEntity{}, store)
	if err != nil {
		t.Fatalf("ResolveComparisonRefs() error: %v", err)
	}
	if resolver.IsOperationComparisonFromCache(p1Id) {
		t.Errorf("a comparison stored by a different builder version must not count as cache")
	}
	if _, ok := resolver.StoredComparison(p1Id); ok {
		t.Errorf("a comparison stored by a different builder version must not be exposed")
	}
}

func TestResolveComparisonRefs_nestedDashboardGetsOwnRefs(t *testing.T) {
	// outer dashboard d references inner dashboard i, which references leaf package p
	reader := &BuildResultToEntitiesReader{
		BuildResultArchive: &BuildResultArchive{
			PackageInfo: view.PackageInfoFile{
				PackageId: "d", Version: "v2", Revision: 1,
				PreviousVersion: "v1", PreviousVersionRevision: 1,
				BuilderVersion: "builder-1",
			},
			PackageComparisons: view.PackageComparisonsFile{
				Comparisons: []view.VersionComparison{
					{PackageId: "d", Version: "v2", Revision: 1, PreviousVersionPackageId: "d", PreviousVersion: "v1", PreviousVersionRevision: 1},
					{PackageId: "i", Version: "v2", Revision: 1, PreviousVersionPackageId: "i", PreviousVersion: "v1", PreviousVersionRevision: 1},
					{PackageId: "p", Version: "v2", Revision: 1, PreviousVersionPackageId: "p", PreviousVersion: "v1", PreviousVersionRevision: 1},
				},
			},
		},
	}
	iId := comparisonId(vk("i", "v2", 1), vk("i", "v1", 1))
	pId := comparisonId(vk("p", "v2", 1), vk("p", "v1", 1))

	currentRefs := []*entity.PublishedReferenceEntity{
		{RefPackageId: "i", RefVersion: "v2", RefRevision: 1},
		{RefPackageId: "p", RefVersion: "v2", RefRevision: 1, ParentRefPackageId: "i", ParentRefVersion: "v2", ParentRefRevision: 1},
	}
	store := &fakeRefsStore{
		refsByVersion: map[VersionKey][]entity.PublishedReferenceEntity{
			vk("d", "v1", 1): {
				{RefPackageId: "i", RefVersion: "v1", RefRevision: 1},
				{RefPackageId: "p", RefVersion: "v1", RefRevision: 1, ParentRefPackageId: "i", ParentRefVersion: "v1", ParentRefRevision: 1},
			},
		},
	}

	resolver, err := reader.ResolveComparisonRefs(currentRefs, store)
	if err != nil {
		t.Fatalf("ResolveComparisonRefs() error: %v", err)
	}
	if !slices.Equal(resolver.Refs(iId), []string{pId}) {
		t.Errorf("inner dashboard refs = %v, want the leaf comparison", resolver.Refs(iId))
	}
	if resolver.Refs(pId) != nil {
		t.Errorf("leaf comparison refs = %v, want none", resolver.Refs(pId))
	}
}
