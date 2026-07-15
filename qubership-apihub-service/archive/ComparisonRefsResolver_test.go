package archive

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/entity"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/exception"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/repository"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
)

func versionKey(packageId string, version string, revision int) VersionKey {
	return VersionKey{PackageId: packageId, Version: version, Revision: revision}
}

func TestCollectNestedComparisonIds(t *testing.T) {
	dashboard1Cur := versionKey("dashboard1", "v2", 1)
	dashboard1Prev := versionKey("dashboard1", "v1", 1)
	packageCur := versionKey("package", "v2", 1)
	packagePrev := versionKey("package", "v1", 1)
	otherCur := versionKey("other", "v2", 1)

	tests := []struct {
		name                string
		parentComparisonId  string
		candidates          []ComparisonRefCandidate
		currentDescendants  map[VersionKey]struct{}
		previousDescendants map[VersionKey]struct{}
		want                []string
	}{
		{
			name:               "nested package comparison matches by both sides",
			parentComparisonId: "dashboard1-comparison",
			candidates: []ComparisonRefCandidate{
				{ComparisonId: "dashboard1-comparison", Current: dashboard1Cur, Previous: dashboard1Prev},
				{ComparisonId: "package-comparison", Current: packageCur, Previous: packagePrev},
			},
			currentDescendants:  map[VersionKey]struct{}{packageCur: {}},
			previousDescendants: map[VersionKey]struct{}{packagePrev: {}},
			want:                []string{"package-comparison"},
		},
		{
			name:               "added reference matches by current side only",
			parentComparisonId: "dashboard1-comparison",
			candidates: []ComparisonRefCandidate{
				{ComparisonId: "added-comparison", Current: packageCur},
			},
			currentDescendants:  map[VersionKey]struct{}{packageCur: {}},
			previousDescendants: map[VersionKey]struct{}{},
			want:                []string{"added-comparison"},
		},
		{
			name:               "removed reference matches by previous side only",
			parentComparisonId: "dashboard1-comparison",
			candidates: []ComparisonRefCandidate{
				{ComparisonId: "removed-comparison", Previous: packagePrev},
			},
			currentDescendants:  map[VersionKey]struct{}{},
			previousDescendants: map[VersionKey]struct{}{packagePrev: {}},
			want:                []string{"removed-comparison"},
		},
		{
			name:               "candidate with a foreign previous side does not match",
			parentComparisonId: "dashboard1-comparison",
			candidates: []ComparisonRefCandidate{
				{ComparisonId: "cross-comparison", Current: packageCur, Previous: versionKey("package", "v0", 1)},
			},
			currentDescendants:  map[VersionKey]struct{}{packageCur: {}},
			previousDescendants: map[VersionKey]struct{}{packagePrev: {}},
			want:                nil,
		},
		{
			name:               "removed candidate does not match when the same package exists on the current side",
			parentComparisonId: "dashboard1-comparison",
			candidates: []ComparisonRefCandidate{
				{ComparisonId: "foreign-removed-comparison", Previous: packagePrev},
			},
			currentDescendants:  map[VersionKey]struct{}{packageCur: {}},
			previousDescendants: map[VersionKey]struct{}{packagePrev: {}},
			want:                nil,
		},
		{
			name:               "added candidate does not match when the same package existed on the previous side",
			parentComparisonId: "dashboard1-comparison",
			candidates: []ComparisonRefCandidate{
				{ComparisonId: "foreign-added-comparison", Current: packageCur},
			},
			currentDescendants:  map[VersionKey]struct{}{packageCur: {}},
			previousDescendants: map[VersionKey]struct{}{packagePrev: {}},
			want:                nil,
		},
		{
			name:               "removed candidate matches when a different package is on the current side",
			parentComparisonId: "dashboard1-comparison",
			candidates: []ComparisonRefCandidate{
				{ComparisonId: "removed-comparison", Previous: packagePrev},
			},
			currentDescendants:  map[VersionKey]struct{}{otherCur: {}},
			previousDescendants: map[VersionKey]struct{}{packagePrev: {}},
			want:                []string{"removed-comparison"},
		},
		{
			name:               "added candidate matches when a different package is on the previous side",
			parentComparisonId: "dashboard1-comparison",
			candidates: []ComparisonRefCandidate{
				{ComparisonId: "added-comparison", Current: packageCur},
			},
			currentDescendants:  map[VersionKey]struct{}{packageCur: {}},
			previousDescendants: map[VersionKey]struct{}{versionKey("other", "v1", 1): {}},
			want:                []string{"added-comparison"},
		},
		{
			name:               "candidate outside both trees does not match",
			parentComparisonId: "dashboard1-comparison",
			candidates: []ComparisonRefCandidate{
				{ComparisonId: "other-comparison", Current: otherCur},
			},
			currentDescendants:  map[VersionKey]struct{}{packageCur: {}},
			previousDescendants: map[VersionKey]struct{}{packagePrev: {}},
			want:                nil,
		},
		{
			name:               "candidate with both sides empty never matches",
			parentComparisonId: "dashboard1-comparison",
			candidates: []ComparisonRefCandidate{
				{ComparisonId: "empty-comparison"},
			},
			currentDescendants:  map[VersionKey]struct{}{packageCur: {}},
			previousDescendants: map[VersionKey]struct{}{packagePrev: {}},
			want:                nil,
		},
		{
			name:               "parent itself is excluded",
			parentComparisonId: "package-comparison",
			candidates: []ComparisonRefCandidate{
				{ComparisonId: "package-comparison", Current: packageCur, Previous: packagePrev},
			},
			currentDescendants:  map[VersionKey]struct{}{packageCur: {}},
			previousDescendants: map[VersionKey]struct{}{packagePrev: {}},
			want:                nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CollectNestedComparisonIds(tt.parentComparisonId, tt.candidates, tt.currentDescendants, tt.previousDescendants)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("CollectNestedComparisonIds() = %v, want %v", got, tt.want)
			}
		})
	}
}

func reference(refPackageId string, refVersion string, refRevision int, parent VersionKey) entity.PublishedReferenceEntity {
	return entity.PublishedReferenceEntity{
		RefPackageId:       refPackageId,
		RefVersion:         refVersion,
		RefRevision:        refRevision,
		ParentRefPackageId: parent.PackageId,
		ParentRefVersion:   parent.Version,
		ParentRefRevision:  parent.Revision,
	}
}

func TestRefTreeDescendants(t *testing.T) {
	root := versionKey("dashboard2", "v2", 1)
	dashboard1 := versionKey("dashboard1", "v2", 1)
	dashboard1a := versionKey("dashboard1a", "v2", 1)
	pkg := versionKey("package", "v2", 1)
	pkgA := versionKey("package-a", "v2", 1)

	tree := newRefTree(root)
	tree.addReference(reference("dashboard1", "v2", 1, VersionKey{}))
	tree.addReference(reference("dashboard1a", "v2", 1, VersionKey{}))
	tree.addReference(reference("package", "v2", 1, dashboard1))
	tree.addReference(reference("package-a", "v2", 1, dashboard1a))

	tests := []struct {
		name string
		node VersionKey
		want map[VersionKey]struct{}
	}{
		{name: "root sees the whole tree", node: root, want: map[VersionKey]struct{}{dashboard1: {}, dashboard1a: {}, pkg: {}, pkgA: {}}},
		{name: "nested dashboard sees its subtree", node: dashboard1, want: map[VersionKey]struct{}{pkg: {}}},
		{name: "second nested dashboard sees its subtree", node: dashboard1a, want: map[VersionKey]struct{}{pkgA: {}}},
		{name: "leaf has no descendants", node: pkg, want: map[VersionKey]struct{}{}},
		{name: "empty node has no descendants", node: VersionKey{}, want: map[VersionKey]struct{}{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tree.descendants(tt.node)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("descendants(%v) = %v, want %v", tt.node, got, tt.want)
			}
		})
	}
}

// fakePublishedRepo overrides only the methods the resolver uses; any other call panics.
type fakePublishedRepo struct {
	repository.PublishedRepository
	storedComparisons []entity.VersionComparisonEntity
	comparisonQueries [][]string
	refsByVersion     map[VersionKey][]entity.PublishedReferenceEntity
	refsQueries       []VersionKey
}

func (f *fakePublishedRepo) GetVersionComparisonsByIds(comparisonIds []string) ([]entity.VersionComparisonEntity, error) {
	f.comparisonQueries = append(f.comparisonQueries, append([]string(nil), comparisonIds...))
	result := make([]entity.VersionComparisonEntity, 0)
	for _, stored := range f.storedComparisons {
		for _, id := range comparisonIds {
			if stored.ComparisonId == id {
				result = append(result, stored)
			}
		}
	}
	return result, nil
}

func (f *fakePublishedRepo) GetVersionRefsV3(packageId string, version string, revision int) ([]entity.PublishedReferenceEntity, error) {
	key := versionKey(packageId, version, revision)
	f.refsQueries = append(f.refsQueries, key)
	return f.refsByVersion[key], nil
}

// dashboardChainReader builds a reader for a nested dashboard chain: dashboard2 -> dashboard1 ->
// package, with each level published as v2 against v1 as the previous version.
func dashboardChainReader() *BuildResultToEntitiesReader {
	return &BuildResultToEntitiesReader{BuildResultArchive: &BuildResultArchive{
		PackageInfo: view.PackageInfoFile{
			PackageId:      "dashboard2",
			Version:        "v2",
			Revision:       1,
			BuilderVersion: "3.0.0",
		},
		PackageComparisons: view.PackageComparisonsFile{Comparisons: []view.VersionComparison{
			{PackageId: "dashboard2", Version: "v2", Revision: 1, PreviousVersionPackageId: "dashboard2", PreviousVersion: "v1", PreviousVersionRevision: 1},
			{PackageId: "dashboard1", Version: "v2", Revision: 1, PreviousVersionPackageId: "dashboard1", PreviousVersion: "v1", PreviousVersionRevision: 1},
			{PackageId: "package", Version: "v2", Revision: 1, PreviousVersionPackageId: "package", PreviousVersion: "v1", PreviousVersionRevision: 1},
		}},
	}}
}

func makeComparisonId(packageId string, version string, revision int, previousPackageId string, previousVersion string, previousRevision int) string {
	return view.MakeVersionComparisonId(packageId, version, revision, previousPackageId, previousVersion, previousRevision)
}

func TestResolveComparisonRefs(t *testing.T) {
	mainId := makeComparisonId("dashboard2", "v2", 1, "dashboard2", "v1", 1)
	dashboard1Id := makeComparisonId("dashboard1", "v2", 1, "dashboard1", "v1", 1)
	packageId := makeComparisonId("package", "v2", 1, "package", "v1", 1)

	currentVersionRefs := []*entity.PublishedReferenceEntity{
		{RefPackageId: "dashboard1", RefVersion: "v2", RefRevision: 1},
		{RefPackageId: "package", RefVersion: "v2", RefRevision: 1, ParentRefPackageId: "dashboard1", ParentRefVersion: "v2", ParentRefRevision: 1},
	}
	previousVersionRefs := []entity.PublishedReferenceEntity{
		{RefPackageId: "dashboard1", RefVersion: "v1", RefRevision: 1},
		{RefPackageId: "package", RefVersion: "v1", RefRevision: 1, ParentRefPackageId: "dashboard1", ParentRefVersion: "v1", ParentRefRevision: 1},
	}

	t.Run("new nested comparisons get their own refs", func(t *testing.T) {
		repo := &fakePublishedRepo{refsByVersion: map[VersionKey][]entity.PublishedReferenceEntity{
			versionKey("dashboard2", "v1", 1): previousVersionRefs,
		}}
		resolver, err := dashboardChainReader().ResolveComparisonRefs(currentVersionRefs, repo)
		if err != nil {
			t.Fatalf("ResolveComparisonRefs() error = %v", err)
		}
		if got := resolver.Refs(mainId); !sameElements(got, []string{dashboard1Id, packageId}) {
			t.Errorf("main refs = %v, want %v", got, []string{dashboard1Id, packageId})
		}
		if got := resolver.Refs(dashboard1Id); !sameElements(got, []string{packageId}) {
			t.Errorf("dashboard1 refs = %v, want %v", got, []string{packageId})
		}
		if got := resolver.Refs(packageId); got != nil {
			t.Errorf("package refs = %v, want nil", got)
		}
		if resolver.IsOperationComparisonFromCache(dashboard1Id) || resolver.IsOperationComparisonFromCache(mainId) {
			t.Errorf("operation comparisons must be rebuilt")
		}
		if got := resolver.OperationComparisonIdsToRebuild(); !reflect.DeepEqual(got, []string{mainId, dashboard1Id, packageId}) {
			t.Errorf("operation comparison ids to rebuild = %v", got)
		}
		if !reflect.DeepEqual(repo.comparisonQueries, [][]string{{dashboard1Id, packageId}}) {
			t.Errorf("stored comparison queries = %v, want non-main ids only", repo.comparisonQueries)
		}
	})

	t.Run("comparison stored with the same builder version is skipped", func(t *testing.T) {
		repo := &fakePublishedRepo{
			storedComparisons: []entity.VersionComparisonEntity{{ComparisonId: dashboard1Id, BuilderVersion: "3.0.0", OperationTypes: []view.OperationType{}}},
			refsByVersion: map[VersionKey][]entity.PublishedReferenceEntity{
				versionKey("dashboard2", "v1", 1): previousVersionRefs,
			},
		}
		resolver, err := dashboardChainReader().ResolveComparisonRefs(currentVersionRefs, repo)
		if err != nil {
			t.Fatalf("ResolveComparisonRefs() error = %v", err)
		}
		if !resolver.IsOperationComparisonFromCache(dashboard1Id) {
			t.Errorf("IsOperationComparisonFromCache(dashboard1) = false, want true")
		}
		if got := resolver.OperationComparisonIdsToRebuild(); !reflect.DeepEqual(got, []string{mainId, packageId}) {
			t.Errorf("operation comparison ids to rebuild = %v, want %v", got, []string{mainId, packageId})
		}
		// the stored comparison stays referenced by the main one
		if got := resolver.Refs(mainId); !sameElements(got, []string{dashboard1Id, packageId}) {
			t.Errorf("main refs = %v, want %v", got, []string{dashboard1Id, packageId})
		}
		if got := resolver.SkippedVersionComparisonIds(); !reflect.DeepEqual(got, []string{dashboard1Id}) {
			t.Errorf("skipped comparison ids = %v, want %v", got, []string{dashboard1Id})
		}
	})

	t.Run("comparison stored with another builder version is re-inserted", func(t *testing.T) {
		repo := &fakePublishedRepo{
			storedComparisons: []entity.VersionComparisonEntity{{ComparisonId: dashboard1Id, BuilderVersion: "2.0.0"}},
			refsByVersion: map[VersionKey][]entity.PublishedReferenceEntity{
				versionKey("dashboard2", "v1", 1): previousVersionRefs,
			},
		}
		resolver, err := dashboardChainReader().ResolveComparisonRefs(currentVersionRefs, repo)
		if err != nil {
			t.Fatalf("ResolveComparisonRefs() error = %v", err)
		}
		if resolver.IsOperationComparisonFromCache(dashboard1Id) {
			t.Errorf("IsOperationComparisonFromCache(dashboard1) = true, want false")
		}
		if got := resolver.Refs(dashboard1Id); !sameElements(got, []string{packageId}) {
			t.Errorf("dashboard1 refs = %v, want %v", got, []string{packageId})
		}
	})

	t.Run("from cache comparison is skipped but stays in refs", func(t *testing.T) {
		reader := dashboardChainReader()
		reader.PackageComparisons.Comparisons[2].FromCache = true
		repo := &fakePublishedRepo{
			storedComparisons: []entity.VersionComparisonEntity{{ComparisonId: packageId, BuilderVersion: "3.0.0", OperationTypes: []view.OperationType{}}},
			refsByVersion: map[VersionKey][]entity.PublishedReferenceEntity{
				versionKey("dashboard2", "v1", 1): previousVersionRefs,
			},
		}
		resolver, err := reader.ResolveComparisonRefs(currentVersionRefs, repo)
		if err != nil {
			t.Fatalf("ResolveComparisonRefs() error = %v", err)
		}
		if !resolver.IsOperationComparisonFromCache(packageId) {
			t.Errorf("package operation comparison must be read from cache")
		}
		if got := resolver.Refs(dashboard1Id); !sameElements(got, []string{packageId}) {
			t.Errorf("dashboard1 refs = %v, want %v", got, []string{packageId})
		}
		if got := resolver.Refs(mainId); !sameElements(got, []string{dashboard1Id, packageId}) {
			t.Errorf("main refs = %v, want %v", got, []string{dashboard1Id, packageId})
		}
		if got := resolver.SkippedVersionComparisonIds(); !reflect.DeepEqual(got, []string{packageId}) {
			t.Errorf("skipped comparison ids = %v, want %v", got, []string{packageId})
		}
	})

	t.Run("operation cache does not suppress ddl rebuild", func(t *testing.T) {
		reader := dashboardChainReader()
		reader.PackageDdlComparisons.Comparisons = []view.DdlVersionComparison{
			{PackageId: "dashboard1", Version: "v2", Revision: 1, PreviousVersionPackageId: "dashboard1", PreviousVersion: "v1", PreviousVersionRevision: 1},
		}
		repo := &fakePublishedRepo{
			storedComparisons: []entity.VersionComparisonEntity{{ComparisonId: dashboard1Id, BuilderVersion: "3.0.0", OperationTypes: []view.OperationType{}}},
			refsByVersion: map[VersionKey][]entity.PublishedReferenceEntity{
				versionKey("dashboard2", "v1", 1): previousVersionRefs,
			},
		}
		resolver, err := reader.ResolveComparisonRefs(currentVersionRefs, repo)
		if err != nil {
			t.Fatalf("ResolveComparisonRefs() error = %v", err)
		}
		if !resolver.IsOperationComparisonFromCache(dashboard1Id) {
			t.Errorf("dashboard1 operation comparison must be cached")
		}
		if resolver.IsDdlComparisonFromCache(dashboard1Id) {
			t.Errorf("dashboard1 DDL comparison must be rebuilt")
		}
		if got := resolver.DdlComparisonIdsToRebuild(); !reflect.DeepEqual(got, []string{dashboard1Id}) {
			t.Errorf("DDL comparison ids to rebuild = %v, want %v", got, []string{dashboard1Id})
		}
		if got := resolver.Refs(dashboard1Id); !sameElements(got, []string{packageId}) {
			t.Errorf("dashboard1 refs = %v, want %v", got, []string{packageId})
		}
	})

	t.Run("ddl cache does not suppress operation rebuild", func(t *testing.T) {
		reader := dashboardChainReader()
		reader.PackageDdlComparisons.Comparisons = []view.DdlVersionComparison{
			{PackageId: "dashboard1", Version: "v2", Revision: 1, PreviousVersionPackageId: "dashboard1", PreviousVersion: "v1", PreviousVersionRevision: 1, FromCache: true},
		}
		repo := &fakePublishedRepo{
			storedComparisons: []entity.VersionComparisonEntity{{ComparisonId: dashboard1Id, BuilderVersion: "3.0.0", ContractTypes: []view.ContractType{}}},
			refsByVersion: map[VersionKey][]entity.PublishedReferenceEntity{
				versionKey("dashboard2", "v1", 1): previousVersionRefs,
			},
		}
		resolver, err := reader.ResolveComparisonRefs(currentVersionRefs, repo)
		if err != nil {
			t.Fatalf("ResolveComparisonRefs() error = %v", err)
		}
		if resolver.IsOperationComparisonFromCache(dashboard1Id) {
			t.Errorf("dashboard1 operation comparison must be rebuilt")
		}
		if !resolver.IsDdlComparisonFromCache(dashboard1Id) {
			t.Errorf("dashboard1 DDL comparison must be cached")
		}
	})

	t.Run("main comparison is rebuilt when builder does not mark it from cache", func(t *testing.T) {
		repo := &fakePublishedRepo{
			storedComparisons: []entity.VersionComparisonEntity{{ComparisonId: mainId, BuilderVersion: "3.0.0", OperationTypes: []view.OperationType{}}},
			refsByVersion: map[VersionKey][]entity.PublishedReferenceEntity{
				versionKey("dashboard2", "v1", 1): previousVersionRefs,
			},
		}
		resolver, err := dashboardChainReader().ResolveComparisonRefs(currentVersionRefs, repo)
		if err != nil {
			t.Fatalf("ResolveComparisonRefs() error = %v", err)
		}
		if resolver.IsOperationComparisonFromCache(mainId) {
			t.Errorf("main operation comparison must be rebuilt")
		}
	})

	t.Run("main comparison cannot be cached", func(t *testing.T) {
		reader := dashboardChainReader()
		reader.PackageComparisons.Comparisons[0].FromCache = true
		repo := &fakePublishedRepo{
			refsByVersion: map[VersionKey][]entity.PublishedReferenceEntity{
				versionKey("dashboard2", "v1", 1): previousVersionRefs,
			},
		}
		resolver, err := reader.ResolveComparisonRefs(currentVersionRefs, repo)
		if err != nil {
			t.Fatalf("ResolveComparisonRefs() error = %v", err)
		}
		if resolver.IsOperationComparisonFromCache(mainId) {
			t.Errorf("main operation comparison must be rebuilt")
		}
		versionComparisons, _, _, err := reader.ReadOperationComparisonsToEntities(nil, nil, resolver)
		if err != nil {
			t.Fatalf("ReadOperationComparisonsToEntities() error = %v", err)
		}
		foundMain := false
		for _, comparison := range versionComparisons {
			if comparison.ComparisonId == mainId {
				foundMain = true
				break
			}
		}
		if !foundMain {
			t.Errorf("main version comparison parent was not produced")
		}
	})

	t.Run("builder cache requires compatible stored operation data", func(t *testing.T) {
		reader := dashboardChainReader()
		reader.PackageComparisons.Comparisons[2].FromCache = true
		_, err := reader.ResolveComparisonRefs(currentVersionRefs, &fakePublishedRepo{})
		if err == nil {
			t.Fatal("ResolveComparisonRefs() error = nil, want invalid package error")
		}
		var customErr *exception.CustomError
		if !errors.As(err, &customErr) || customErr.Code != exception.InvalidPackagedFile {
			t.Errorf("ResolveComparisonRefs() error = %v, want %s", err, exception.InvalidPackagedFile)
		}
	})

	t.Run("builder cache requires compatible stored ddl data", func(t *testing.T) {
		reader := dashboardChainReader()
		reader.PackageDdlComparisons.Comparisons = []view.DdlVersionComparison{
			{PackageId: "dashboard1", Version: "v2", Revision: 1, PreviousVersionPackageId: "dashboard1", PreviousVersion: "v1", PreviousVersionRevision: 1, FromCache: true},
		}
		_, err := reader.ResolveComparisonRefs(currentVersionRefs, &fakePublishedRepo{})
		if err == nil {
			t.Fatal("ResolveComparisonRefs() error = nil, want invalid package error")
		}
		var customErr *exception.CustomError
		if !errors.As(err, &customErr) || customErr.Code != exception.InvalidPackagedFile {
			t.Errorf("ResolveComparisonRefs() error = %v, want %s", err, exception.InvalidPackagedFile)
		}
	})

	t.Run("current references are read from the DB when not provided", func(t *testing.T) {
		repo := &fakePublishedRepo{refsByVersion: map[VersionKey][]entity.PublishedReferenceEntity{
			versionKey("dashboard2", "v2", 1): {
				{RefPackageId: "dashboard1", RefVersion: "v2", RefRevision: 1},
				{RefPackageId: "package", RefVersion: "v2", RefRevision: 1, ParentRefPackageId: "dashboard1", ParentRefVersion: "v2", ParentRefRevision: 1},
			},
			versionKey("dashboard2", "v1", 1): previousVersionRefs,
		}}
		resolver, err := dashboardChainReader().ResolveComparisonRefs(nil, repo)
		if err != nil {
			t.Fatalf("ResolveComparisonRefs() error = %v", err)
		}
		if got := resolver.Refs(dashboard1Id); !sameElements(got, []string{packageId}) {
			t.Errorf("dashboard1 refs = %v, want %v", got, []string{packageId})
		}
		if !sameElements(refsQueryIds(repo.refsQueries), []string{"dashboard2|v2|1", "dashboard2|v1|1"}) {
			t.Errorf("reference queries = %v, want current and previous versions", repo.refsQueries)
		}
	})

	t.Run("ddl only comparison lands in main refs", func(t *testing.T) {
		reader := dashboardChainReader()
		reader.PackageDdlComparisons = view.PackageDdlComparisonsFile{Comparisons: []view.DdlVersionComparison{
			{PackageId: "ddlpackage", Version: "v2", Revision: 1, PreviousVersionPackageId: "ddlpackage", PreviousVersion: "v1", PreviousVersionRevision: 1},
		}}
		ddlComparisonId := makeComparisonId("ddlpackage", "v2", 1, "ddlpackage", "v1", 1)
		ddlCurrentVersionRefs := append([]*entity.PublishedReferenceEntity{}, currentVersionRefs...)
		ddlCurrentVersionRefs = append(ddlCurrentVersionRefs, &entity.PublishedReferenceEntity{
			RefPackageId: "ddlpackage",
			RefVersion:   "v2",
			RefRevision:  1,
		})
		ddlPreviousVersionRefs := append([]entity.PublishedReferenceEntity{}, previousVersionRefs...)
		ddlPreviousVersionRefs = append(ddlPreviousVersionRefs, entity.PublishedReferenceEntity{
			RefPackageId: "ddlpackage",
			RefVersion:   "v1",
			RefRevision:  1,
		})
		repo := &fakePublishedRepo{refsByVersion: map[VersionKey][]entity.PublishedReferenceEntity{
			versionKey("dashboard2", "v1", 1): ddlPreviousVersionRefs,
		}}
		resolver, err := reader.ResolveComparisonRefs(ddlCurrentVersionRefs, repo)
		if err != nil {
			t.Fatalf("ResolveComparisonRefs() error = %v", err)
		}
		if got := resolver.Refs(mainId); !sameElements(got, []string{dashboard1Id, packageId, ddlComparisonId}) {
			t.Errorf("main refs = %v, want %v", got, []string{dashboard1Id, packageId, ddlComparisonId})
		}
	})
}

func refsQueryIds(queries []VersionKey) []string {
	ids := make([]string, 0, len(queries))
	for _, query := range queries {
		ids = append(ids, fmt.Sprintf("%s|%s|%d", query.PackageId, query.Version, query.Revision))
	}
	return ids
}

func sameElements(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	gotSorted := append([]string(nil), got...)
	wantSorted := append([]string(nil), want...)
	sort.Strings(gotSorted)
	sort.Strings(wantSorted)
	return reflect.DeepEqual(gotSorted, wantSorted)
}
