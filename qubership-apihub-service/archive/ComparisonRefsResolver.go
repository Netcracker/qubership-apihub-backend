package archive

import (
	"net/http"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/entity"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/exception"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/repository"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
)

type VersionKey struct {
	PackageId string
	Version   string
	Revision  int
}

func (k VersionKey) isEmpty() bool {
	return k == VersionKey{}
}

type ComparisonRefCandidate struct {
	ComparisonId string
	Current      VersionKey
	Previous     VersionKey
}

// CollectNestedComparisonIds returns the ids of candidates nested under the comparison identified by
// parentComparisonId. A dashboard comparison stores its whole subtree of comparisons as refs, so a
// candidate is nested when each of its non-empty sides compares a version the parent references
// (directly or transitively). currentDescendants and previousDescendants are the versions referenced
// by the parent's current and previous sides. A one-sided candidate (an added or removed reference) is
// matched by its single non-empty side, and only when the parent's opposite side holds no version of
// that package. A dashboard references each package at a single version, so a package still present on
// the opposite side was changed rather than added or removed, and the one-sided candidate belongs to a
// different parent that merely shares the referenced version.
func CollectNestedComparisonIds(parentComparisonId string, candidates []ComparisonRefCandidate, currentDescendants map[VersionKey]struct{}, previousDescendants map[VersionKey]struct{}) []string {
	currentPackageIds := packageIds(currentDescendants)
	previousPackageIds := packageIds(previousDescendants)
	var refs []string
	for _, candidate := range candidates {
		if candidate.ComparisonId == parentComparisonId {
			continue
		}
		if candidate.Current.isEmpty() && candidate.Previous.isEmpty() {
			continue
		}
		if !candidate.Current.isEmpty() {
			if _, ok := currentDescendants[candidate.Current]; !ok {
				continue
			}
		} else if _, ok := currentPackageIds[candidate.Previous.PackageId]; ok {
			// removed candidate whose package still exists on the parent's current side: the parent
			// changed the package instead of removing it, so this comparison is a different parent's
			continue
		}
		if !candidate.Previous.isEmpty() {
			if _, ok := previousDescendants[candidate.Previous]; !ok {
				continue
			}
		} else if _, ok := previousPackageIds[candidate.Current.PackageId]; ok {
			// added candidate whose package already existed on the parent's previous side: same reasoning
			continue
		}
		refs = append(refs, candidate.ComparisonId)
	}
	return refs
}

func packageIds(descendants map[VersionKey]struct{}) map[string]struct{} {
	ids := make(map[string]struct{}, len(descendants))
	for descendant := range descendants {
		ids[descendant.PackageId] = struct{}{}
	}
	return ids
}

// refTree is the flattened reference set of one published version
type refTree struct {
	root             VersionKey
	childrenByParent map[VersionKey][]VersionKey
	descendantsCache map[VersionKey]map[VersionKey]struct{}
}

func newRefTree(root VersionKey) *refTree {
	return &refTree{
		root:             root,
		childrenByParent: map[VersionKey][]VersionKey{},
		descendantsCache: map[VersionKey]map[VersionKey]struct{}{},
	}
}

func (t *refTree) addReference(ref entity.PublishedReferenceEntity) {
	child := VersionKey{PackageId: ref.RefPackageId, Version: ref.RefVersion, Revision: ref.RefRevision}
	parent := VersionKey{PackageId: ref.ParentRefPackageId, Version: ref.ParentRefVersion, Revision: ref.ParentRefRevision}
	if parent.isEmpty() {
		parent = t.root
	}
	t.childrenByParent[parent] = append(t.childrenByParent[parent], child)
}

// descendants returns every version reachable from node through the reference tree
func (t *refTree) descendants(node VersionKey) map[VersionKey]struct{} {
	if cached, ok := t.descendantsCache[node]; ok {
		return cached
	}
	result := make(map[VersionKey]struct{})
	t.collectDescendants(node, result)
	t.descendantsCache[node] = result
	return result
}

func (t *refTree) collectDescendants(node VersionKey, result map[VersionKey]struct{}) {
	for _, child := range t.childrenByParent[node] {
		if _, visited := result[child]; visited {
			continue
		}
		result[child] = struct{}{}
		t.collectDescendants(child, result)
	}
}

// ComparisonRefsResolver answers two questions for every comparison in the build archive:
// which refs the comparison must be stored with, and whether it is already present in the DB
// and therefore must not be re-inserted.
type ComparisonRefsResolver struct {
	refsByComparisonId map[string][]string
	cachedIds          map[string]struct{}
}

func (r *ComparisonRefsResolver) IsCached(comparisonId string) bool {
	_, cached := r.cachedIds[comparisonId]
	return cached
}

func (r *ComparisonRefsResolver) Refs(comparisonId string) []string {
	return r.refsByComparisonId[comparisonId]
}

func (r *ComparisonRefsResolver) CachedComparisonIds() []string {
	cachedIds := make([]string, 0, len(r.cachedIds))
	for id := range r.cachedIds {
		cachedIds = append(cachedIds, id)
	}
	return cachedIds
}

func (a *BuildResultToEntitiesReader) constructComparisonKey(packageId string, version string, revision int, previousVersionPackageId string, previousVersion string, previousVersionRevision int) (view.ComparisonKey, bool) {
	key := view.ComparisonKey{}
	mainVersion := false
	if version != "" {
		if (a.PackageInfo.Revision == revision || revision == 0) &&
			a.PackageInfo.Version == version &&
			a.PackageInfo.PackageId == packageId {
			mainVersion = true
			key.PackageId = packageId
			key.Version = a.PackageInfo.Version
			key.Revision = a.PackageInfo.Revision
		} else {
			key.PackageId = packageId
			key.Version = version
			key.Revision = revision
		}
	}
	if previousVersion != "" {
		key.PreviousVersionPackageId = previousVersionPackageId
		key.PreviousVersion = previousVersion
		key.PreviousVersionRevision = previousVersionRevision
	}
	return key, mainVersion
}

// ResolveComparisonRefs builds the resolver for all comparisons of the build archive
//
// currentVersionRefs are the resolved references of the version being published; pass nil when
// that version is already published (changelog builds) and the references must be read from the DB.
// An empty non-nil slice means the version has no references.
func (a *BuildResultToEntitiesReader) ResolveComparisonRefs(currentVersionRefs []*entity.PublishedReferenceEntity, publishedRepo repository.PublishedRepository) (*ComparisonRefsResolver, error) {
	resolver := &ComparisonRefsResolver{
		refsByComparisonId: map[string][]string{},
		cachedIds:          map[string]struct{}{},
	}

	type comparisonInfo struct {
		ComparisonRefCandidate
		main      bool
		fromCache bool
	}

	archiveComparisonsCount := len(a.PackageComparisons.Comparisons) + len(a.PackageDdlComparisons.Comparisons)
	comparisons := make([]*comparisonInfo, 0, archiveComparisonsCount)
	comparisonById := map[string]*comparisonInfo{}
	var mainComparison *comparisonInfo
	nonMainIds := make([]string, 0, archiveComparisonsCount)
	createComparisonInfo := func(key view.ComparisonKey, main bool, fromCache bool) {
		id := key.ComparisonId()
		if existing, ok := comparisonById[id]; ok {
			existing.fromCache = existing.fromCache && fromCache //TODO: is it possible to have different values for one version comparison?
			return
		}
		info := &comparisonInfo{
			ComparisonRefCandidate: ComparisonRefCandidate{
				ComparisonId: id,
				Current:      VersionKey{PackageId: key.PackageId, Version: key.Version, Revision: key.Revision},
				Previous:     VersionKey{PackageId: key.PreviousVersionPackageId, Version: key.PreviousVersion, Revision: key.PreviousVersionRevision},
			},
			main:      main,
			fromCache: fromCache,
		}
		comparisonById[id] = info
		comparisons = append(comparisons, info)
		if main {
			mainComparison = info
		} else {
			nonMainIds = append(nonMainIds, id)
		}
	}
	for _, comparison := range a.PackageComparisons.Comparisons {
		key, mainVersion := a.constructComparisonKey(comparison.PackageId, comparison.Version, comparison.Revision,
			comparison.PreviousVersionPackageId, comparison.PreviousVersion, comparison.PreviousVersionRevision)
		createComparisonInfo(key, mainVersion, comparison.FromCache)
	}
	for _, comparison := range a.PackageDdlComparisons.Comparisons {
		key, mainVersion := a.constructComparisonKey(comparison.PackageId, comparison.Version, comparison.Revision,
			comparison.PreviousVersionPackageId, comparison.PreviousVersion, comparison.PreviousVersionRevision)
		createComparisonInfo(key, mainVersion, comparison.FromCache)
	}
	if len(comparisons) == 0 {
		return resolver, nil
	}

	// A non-main comparison already stored with the current builder version keeps its DB row: its refs are deterministic per builder version
	if len(nonMainIds) > 0 {
		storedComparisons, err := publishedRepo.GetVersionComparisonsByIds(nonMainIds)
		if err != nil {
			return nil, &exception.CustomError{
				Status:  http.StatusInternalServerError,
				Message: "Failed to get version comparisons by ids",
				Debug:   err.Error(),
			}
		}
		for _, comparison := range storedComparisons {
			if comparison.BuilderVersion == a.PackageInfo.BuilderVersion {
				resolver.cachedIds[comparison.ComparisonId] = struct{}{}
			}
		}
	}

	// The main comparison keeps flat refs: every non-main comparison is its ref, including cached ones
	if mainComparison != nil {
		resolver.refsByComparisonId[mainComparison.ComparisonId] = nonMainIds
	}

	comparisonsToResolve := make([]*comparisonInfo, 0, len(nonMainIds))
	for _, comparison := range comparisons {
		if comparison.main {
			continue
		}
		if _, cached := resolver.cachedIds[comparison.ComparisonId]; cached {
			continue
		}
		if comparison.fromCache {
			resolver.cachedIds[comparison.ComparisonId] = struct{}{}
			continue
		}
		comparisonsToResolve = append(comparisonsToResolve, comparison)
	}
	if len(comparisonsToResolve) == 0 {
		return resolver, nil
	}

	currentTree := newRefTree(VersionKey{PackageId: a.PackageInfo.PackageId, Version: a.PackageInfo.Version, Revision: a.PackageInfo.Revision})
	if currentVersionRefs != nil {
		for _, ref := range currentVersionRefs {
			currentTree.addReference(*ref)
		}
	} else {
		refs, err := publishedRepo.GetVersionRefsV3(currentTree.root.PackageId, currentTree.root.Version, currentTree.root.Revision)
		if err != nil {
			return nil, &exception.CustomError{
				Status:  http.StatusInternalServerError,
				Message: "Failed to get version references for $packageId-$version-$revision",
				Debug:   err.Error(),
				Params:  map[string]interface{}{"packageId": currentTree.root.PackageId, "version": currentTree.root.Version, "revision": currentTree.root.Revision},
			}
		}
		for _, ref := range refs {
			currentTree.addReference(ref)
		}
	}

	previousTree := newRefTree(VersionKey{})
	if mainComparison != nil && !mainComparison.Previous.isEmpty() {
		previousTree = newRefTree(mainComparison.Previous)
		refs, err := publishedRepo.GetVersionRefsV3(mainComparison.Previous.PackageId, mainComparison.Previous.Version, mainComparison.Previous.Revision)
		if err != nil {
			return nil, &exception.CustomError{
				Status:  http.StatusInternalServerError,
				Message: "Failed to get version references for $packageId-$version-$revision",
				Debug:   err.Error(),
				Params:  map[string]interface{}{"packageId": mainComparison.Previous.PackageId, "version": mainComparison.Previous.Version, "revision": mainComparison.Previous.Revision},
			}
		}
		for _, ref := range refs {
			previousTree.addReference(ref)
		}
	}

	candidates := make([]ComparisonRefCandidate, 0, len(comparisons))
	for _, comparison := range comparisons {
		candidates = append(candidates, comparison.ComparisonRefCandidate)
	}
	for _, comparison := range comparisonsToResolve {
		currentDescendants := currentTree.descendants(comparison.Current)
		previousDescendants := previousTree.descendants(comparison.Previous)
		refs := CollectNestedComparisonIds(comparison.ComparisonId, candidates, currentDescendants, previousDescendants)
		if len(refs) > 0 {
			resolver.refsByComparisonId[comparison.ComparisonId] = refs
		}
	}
	return resolver, nil
}
