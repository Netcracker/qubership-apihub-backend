package archive

import (
	"fmt"
	"net/http"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/entity"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/exception"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
)

// VersionKey identifies one side of a version comparison.
type VersionKey struct {
	PackageId string
	Version   string
	Revision  int
}

func (k VersionKey) isEmpty() bool {
	return k == VersionKey{}
}

// ComparisonRefCandidate is a comparison that may turn out to be nested under a parent
// (dashboard) comparison.
type ComparisonRefCandidate struct {
	ComparisonId string
	Current      VersionKey
	Previous     VersionKey
}

// ComparisonRefsStore is the subset of PublishedRepository the resolver needs.
type ComparisonRefsStore interface {
	GetVersionComparisonsByIds(comparisonIds []string) ([]entity.VersionComparisonEntity, error)
	GetVersionRefsV3(packageId string, version string, revision int) ([]entity.PublishedReferenceEntity, error)
}

// CollectNestedComparisonIds returns the ids of candidates nested under the comparison identified
// by parentComparisonId. A dashboard comparison stores its whole subtree of comparisons as refs,
// so a candidate is nested when each of its non-empty sides compares a version the parent
// references (directly or transitively). currentDescendants and previousDescendants are the
// versions referenced by the parent's current and previous sides. A one-sided candidate (an added
// or removed reference) is matched by its single non-empty side, and only when the parent's
// opposite side holds no version of that package: a dashboard references each package at a single
// version, so a package still present on the opposite side was changed rather than added or
// removed, and the one-sided candidate belongs to a different parent that merely shares the
// referenced version.
func CollectNestedComparisonIds(parentComparisonId string, candidates []ComparisonRefCandidate, currentDescendants map[VersionKey]struct{}, previousDescendants map[VersionKey]struct{}) []string {
	currentPackageIds := descendantPackageIds(currentDescendants)
	previousPackageIds := descendantPackageIds(previousDescendants)
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
			// removed candidate whose package still exists on the parent's current side:
			// the parent changed the package instead of removing it
			continue
		}
		if !candidate.Previous.isEmpty() {
			if _, ok := previousDescendants[candidate.Previous]; !ok {
				continue
			}
		} else if _, ok := previousPackageIds[candidate.Current.PackageId]; ok {
			// added candidate whose package already existed on the parent's previous side:
			// the parent changed the package instead of adding it
			continue
		}
		refs = append(refs, candidate.ComparisonId)
	}
	return refs
}

func descendantPackageIds(descendants map[VersionKey]struct{}) map[string]struct{} {
	ids := make(map[string]struct{}, len(descendants))
	for descendant := range descendants {
		ids[descendant.PackageId] = struct{}{}
	}
	return ids
}

// refTree is the flattened reference set of one published version.
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

// descendants returns every version reachable from node through the reference tree.
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

// ComparisonRefsResolver holds the per-comparison refs and cache state computed once per build
// archive, shared by the operation and DDL comparison readers so both files agree on refs and on
// which comparisons are served from cache.
type ComparisonRefsResolver struct {
	refsByComparisonId                    map[string][]string
	operationComparisonFromCacheById      map[string]bool
	ddlComparisonFromCacheById            map[string]bool
	operationComparisonIdsToRebuild       []string
	ddlComparisonIdsToRebuild             []string
	skippedVersionComparisonIds           []string
	storedVersionComparisonByComparisonId map[string]entity.VersionComparisonEntity
}

func (r *ComparisonRefsResolver) IsOperationComparisonFromCache(comparisonId string) bool {
	return r.operationComparisonFromCacheById[comparisonId]
}

func (r *ComparisonRefsResolver) IsDdlComparisonFromCache(comparisonId string) bool {
	return r.ddlComparisonFromCacheById[comparisonId]
}

func (r *ComparisonRefsResolver) Refs(comparisonId string) []string {
	return r.refsByComparisonId[comparisonId]
}

func (r *ComparisonRefsResolver) OperationComparisonIdsToRebuild() []string {
	return r.operationComparisonIdsToRebuild
}

func (r *ComparisonRefsResolver) DdlComparisonIdsToRebuild() []string {
	return r.ddlComparisonIdsToRebuild
}

// SkippedVersionComparisonIds lists the comparisons whose every side (operations and DDL) is
// served from cache, so no version_comparison row is written for them during this publish.
func (r *ComparisonRefsResolver) SkippedVersionComparisonIds() []string {
	return r.skippedVersionComparisonIds
}

// StoredComparison returns the version comparison already persisted for the same builder version,
// used to keep the cached side's data when only the other side is rebuilt.
func (r *ComparisonRefsResolver) StoredComparison(comparisonId string) (entity.VersionComparisonEntity, bool) {
	comparison, exists := r.storedVersionComparisonByComparisonId[comparisonId]
	return comparison, exists
}

// constructComparisonKey normalizes a comparison entry from the build archive: the entry that
// compares the version being published gets the publishing revision (the archive may carry
// revision 0 for it).
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

// ResolveComparisonRefs builds the resolver for all comparisons of the build archive.
//
// currentVersionRefs are the resolved references of the version being published; pass nil when
// that version is already published (changelog builds) and the references must be read from the
// database. An empty non-nil slice means the version has no references.
func (a *BuildResultToEntitiesReader) ResolveComparisonRefs(currentVersionRefs []*entity.PublishedReferenceEntity, store ComparisonRefsStore) (*ComparisonRefsResolver, error) {
	resolver := &ComparisonRefsResolver{
		refsByComparisonId:                    map[string][]string{},
		operationComparisonFromCacheById:      map[string]bool{},
		ddlComparisonFromCacheById:            map[string]bool{},
		storedVersionComparisonByComparisonId: map[string]entity.VersionComparisonEntity{},
	}

	type comparisonInfo struct {
		ComparisonRefCandidate
		main bool
	}

	archiveComparisonsCount := len(a.PackageComparisons.Comparisons) + len(a.PackageDdlComparisons.Comparisons)
	comparisons := make([]*comparisonInfo, 0, archiveComparisonsCount)
	candidates := make([]ComparisonRefCandidate, 0, archiveComparisonsCount)
	comparisonById := map[string]*comparisonInfo{}
	var mainComparison *comparisonInfo
	nonMainIds := make([]string, 0, archiveComparisonsCount)
	collectComparison := func(key view.ComparisonKey, main bool) {
		id := key.ComparisonId()
		if _, ok := comparisonById[id]; ok {
			return
		}
		info := &comparisonInfo{
			ComparisonRefCandidate: ComparisonRefCandidate{
				ComparisonId: id,
				Current:      VersionKey{PackageId: key.PackageId, Version: key.Version, Revision: key.Revision},
				Previous:     VersionKey{PackageId: key.PreviousVersionPackageId, Version: key.PreviousVersion, Revision: key.PreviousVersionRevision},
			},
			main: main,
		}
		comparisonById[id] = info
		comparisons = append(comparisons, info)
		candidates = append(candidates, info.ComparisonRefCandidate)
		if main {
			mainComparison = info
		} else {
			nonMainIds = append(nonMainIds, id)
		}
	}
	for _, comparison := range a.PackageComparisons.Comparisons {
		key, mainVersion := a.constructComparisonKey(comparison.PackageId, comparison.Version, comparison.Revision,
			comparison.PreviousVersionPackageId, comparison.PreviousVersion, comparison.PreviousVersionRevision)
		collectComparison(key, mainVersion)
	}
	for _, comparison := range a.PackageDdlComparisons.Comparisons {
		key, mainVersion := a.constructComparisonKey(comparison.PackageId, comparison.Version, comparison.Revision,
			comparison.PreviousVersionPackageId, comparison.PreviousVersion, comparison.PreviousVersionRevision)
		collectComparison(key, mainVersion)
	}
	if len(comparisons) == 0 {
		return resolver, nil
	}

	if len(nonMainIds) > 0 {
		storedComparisons, err := store.GetVersionComparisonsByIds(nonMainIds)
		if err != nil {
			return nil, &exception.CustomError{
				Status:  http.StatusInternalServerError,
				Message: "Failed to get version comparisons by ids",
				Debug:   err.Error(),
			}
		}
		for _, comparison := range storedComparisons {
			// a comparison stored by a different builder version cannot back a cached entry
			if comparison.BuilderVersion != a.PackageInfo.BuilderVersion {
				continue
			}
			resolver.storedVersionComparisonByComparisonId[comparison.ComparisonId] = comparison
		}
	}

	for _, comparison := range a.PackageComparisons.Comparisons {
		key, mainVersion := a.constructComparisonKey(comparison.PackageId, comparison.Version, comparison.Revision,
			comparison.PreviousVersionPackageId, comparison.PreviousVersion, comparison.PreviousVersionRevision)
		comparisonId := key.ComparisonId()
		stored, storedExists := resolver.storedVersionComparisonByComparisonId[comparisonId]
		storedOperationComparisonAvailable := storedExists && stored.OperationTypes != nil
		if !mainVersion && comparison.FromCache && !storedOperationComparisonAvailable {
			return nil, invalidCachedComparisonError(ComparisonsFilePath, comparisonId, a.PackageInfo.BuilderVersion)
		}
		fromCache := !mainVersion && (comparison.FromCache || storedOperationComparisonAvailable)
		resolver.operationComparisonFromCacheById[comparisonId] = fromCache
		if !fromCache {
			resolver.operationComparisonIdsToRebuild = append(resolver.operationComparisonIdsToRebuild, comparisonId)
		}
	}

	for _, comparison := range a.PackageDdlComparisons.Comparisons {
		key, mainVersion := a.constructComparisonKey(comparison.PackageId, comparison.Version, comparison.Revision,
			comparison.PreviousVersionPackageId, comparison.PreviousVersion, comparison.PreviousVersionRevision)
		comparisonId := key.ComparisonId()
		stored, storedExists := resolver.storedVersionComparisonByComparisonId[comparisonId]
		storedDdlComparisonAvailable := storedExists && stored.ContractTypes != nil
		if !mainVersion && comparison.FromCache && !storedDdlComparisonAvailable {
			return nil, invalidCachedComparisonError(ContractsDdlComparisonsFilePath, comparisonId, a.PackageInfo.BuilderVersion)
		}
		fromCache := !mainVersion && (comparison.FromCache || storedDdlComparisonAvailable)
		resolver.ddlComparisonFromCacheById[comparisonId] = fromCache
		if !fromCache {
			resolver.ddlComparisonIdsToRebuild = append(resolver.ddlComparisonIdsToRebuild, comparisonId)
		}
	}

	// the main comparison keeps flat refs: every non-main comparison is its ref, including cached ones
	if mainComparison != nil {
		resolver.refsByComparisonId[mainComparison.ComparisonId] = nonMainIds
	}

	comparisonsToResolve := make([]*comparisonInfo, 0, len(nonMainIds))
	for _, comparison := range comparisons {
		operationFromCache, hasOperationComparison := resolver.operationComparisonFromCacheById[comparison.ComparisonId]
		ddlFromCache, hasDdlComparison := resolver.ddlComparisonFromCacheById[comparison.ComparisonId]
		if (!hasOperationComparison || operationFromCache) && (!hasDdlComparison || ddlFromCache) {
			resolver.skippedVersionComparisonIds = append(resolver.skippedVersionComparisonIds, comparison.ComparisonId)
			continue
		}
		if !comparison.main {
			comparisonsToResolve = append(comparisonsToResolve, comparison)
		}
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
		refs, err := store.GetVersionRefsV3(currentTree.root.PackageId, currentTree.root.Version, currentTree.root.Revision)
		if err != nil {
			return nil, versionRefsError(currentTree.root, err)
		}
		for _, ref := range refs {
			currentTree.addReference(ref)
		}
	}

	previousTree := newRefTree(VersionKey{})
	if mainComparison != nil && !mainComparison.Previous.isEmpty() {
		previousTree = newRefTree(mainComparison.Previous)
		refs, err := store.GetVersionRefsV3(mainComparison.Previous.PackageId, mainComparison.Previous.Version, mainComparison.Previous.Revision)
		if err != nil {
			return nil, versionRefsError(mainComparison.Previous, err)
		}
		for _, ref := range refs {
			previousTree.addReference(ref)
		}
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

func invalidCachedComparisonError(filePath string, comparisonId string, builderVersion string) error {
	return &exception.CustomError{
		Status:  http.StatusBadRequest,
		Code:    exception.InvalidPackagedFile,
		Message: exception.InvalidPackagedFileMsg,
		Params: map[string]interface{}{
			"file":  filePath,
			"error": fmt.Sprintf("comparison %q is marked as fromCache, but no stored comparison data exists for builder version %q", comparisonId, builderVersion),
		},
	}
}

func versionRefsError(version VersionKey, err error) error {
	return &exception.CustomError{
		Status:  http.StatusInternalServerError,
		Message: "Failed to get version references for $packageId-$version-$revision",
		Debug:   err.Error(),
		Params:  map[string]interface{}{"packageId": version.PackageId, "version": version.Version, "revision": version.Revision},
	}
}
