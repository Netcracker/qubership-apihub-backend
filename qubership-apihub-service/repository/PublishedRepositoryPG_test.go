package repository

import (
	"reflect"
	"testing"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/entity"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
)

func TestComparisonIdsToRebuild(t *testing.T) {
	got := comparisonIdsToRebuild(
		[]string{"operation-only", "shared"},
		[]string{"ddl-only", "shared"},
	)
	want := []string{"operation-only", "shared", "ddl-only"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("comparisonIdsToRebuild() = %v, want %v", got, want)
	}
}

func TestComparisonIdsToValidate(t *testing.T) {
	got := comparisonIdsToValidate(
		[]string{"operation-cached", "operation-rebuilt"},
		[]string{"operation-rebuilt", "new-operation-comparison"},
	)
	want := []string{"operation-rebuilt"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("comparisonIdsToValidate() = %v, want %v", got, want)
	}
}

func TestMakeComparisonInternalDocumentValidationScope(t *testing.T) {
	matchedKey := view.ComparisonKey{PackageId: "matched", Version: "v2", Revision: 1, PreviousVersionPackageId: "matched", PreviousVersion: "v1", PreviousVersionRevision: 1}
	missingKey := view.ComparisonKey{PackageId: "missing", Version: "v2", Revision: 1, PreviousVersionPackageId: "missing", PreviousVersion: "v1", PreviousVersionRevision: 1}
	cachedKey := view.ComparisonKey{PackageId: "cached", Version: "v2", Revision: 1, PreviousVersionPackageId: "cached", PreviousVersion: "v1", PreviousVersionRevision: 1}
	unexpectedKey := view.ComparisonKey{PackageId: "unexpected", Version: "v2", Revision: 1, PreviousVersionPackageId: "unexpected", PreviousVersion: "v1", PreviousVersionRevision: 1}

	oldComparisons := []entity.VersionComparisonEntity{
		{PackageId: matchedKey.PackageId, Version: matchedKey.Version, Revision: matchedKey.Revision, PreviousPackageId: matchedKey.PreviousVersionPackageId, PreviousVersion: matchedKey.PreviousVersion, PreviousRevision: matchedKey.PreviousVersionRevision, ComparisonId: "cmp-matched"},
		{PackageId: missingKey.PackageId, Version: missingKey.Version, Revision: missingKey.Revision, PreviousPackageId: missingKey.PreviousVersionPackageId, PreviousVersion: missingKey.PreviousVersion, PreviousRevision: missingKey.PreviousVersionRevision, ComparisonId: "cmp-missing"},
		{PackageId: cachedKey.PackageId, Version: cachedKey.Version, Revision: cachedKey.Revision, PreviousPackageId: cachedKey.PreviousVersionPackageId, PreviousVersion: cachedKey.PreviousVersion, PreviousRevision: cachedKey.PreviousVersionRevision, ComparisonId: "cmp-cached"},
	}
	matchedComparison := oldComparisons[0]
	unexpectedComparison := entity.VersionComparisonEntity{
		PackageId:         unexpectedKey.PackageId,
		Version:           unexpectedKey.Version,
		Revision:          unexpectedKey.Revision,
		PreviousPackageId: unexpectedKey.PreviousVersionPackageId,
		PreviousVersion:   unexpectedKey.PreviousVersion,
		PreviousRevision:  unexpectedKey.PreviousVersionRevision,
		ComparisonId:      "cmp-unexpected",
	}

	got := makeComparisonInternalDocumentValidationScope(
		oldComparisons,
		[]*entity.VersionComparisonEntity{&matchedComparison, &unexpectedComparison},
		[]string{"cmp-cached"},
	)
	want := map[view.ComparisonKey]string{
		matchedKey:    "cmp-matched",
		missingKey:    "cmp-missing",
		unexpectedKey: "cmp-unexpected",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("makeComparisonInternalDocumentValidationScope() = %#v, want %#v", got, want)
	}
}

func comparisonInternalDocument(comparison view.ComparisonKey, documentId string, filename string, hash string) entity.ComparisonInternalDocumentEntity {
	return entity.ComparisonInternalDocumentEntity{
		PackageId:         comparison.PackageId,
		Version:           comparison.Version,
		Revision:          comparison.Revision,
		PreviousPackageId: comparison.PreviousVersionPackageId,
		PreviousVersion:   comparison.PreviousVersion,
		PreviousRevision:  comparison.PreviousVersionRevision,
		DocumentId:        documentId,
		Filename:          filename,
		Hash:              hash,
	}
}

func TestReconcileComparisonInternalDocuments(t *testing.T) {
	mainComparison := view.ComparisonKey{PackageId: "pkg", Version: "v2", Revision: 1, PreviousVersionPackageId: "pkg", PreviousVersion: "v1", PreviousVersionRevision: 1}
	newComparison := view.ComparisonKey{PackageId: "mw", Version: "v2", Revision: 1, PreviousVersionPackageId: "mw", PreviousVersion: "v1", PreviousVersionRevision: 1}
	comparisonIdByKey := map[view.ComparisonKey]string{
		mainComparison: "cmp-main",
		newComparison:  "cmp-new",
	}

	oldDocs := []entity.ComparisonInternalDocumentEntity{
		comparisonInternalDocument(mainComparison, "unchanged", "f-unchanged", "h-unchanged"),
		comparisonInternalDocument(mainComparison, "changed", "f-changed", "h-old"),
		comparisonInternalDocument(mainComparison, "db-only", "f-db-only", "h-db-only"),
	}
	unchanged := comparisonInternalDocument(mainComparison, "unchanged", "f-unchanged", "h-unchanged")
	changed := comparisonInternalDocument(mainComparison, "changed", "f-changed", "h-new")
	archiveOnly := comparisonInternalDocument(newComparison, "archive-only", "f-archive-only", "h-archive-only")
	newDocs := []*entity.ComparisonInternalDocumentEntity{&unchanged, &changed, &archiveOnly}
	newDocData := []*entity.ComparisonInternalDocumentDataEntity{
		{Hash: "h-unchanged"},
		{Hash: "h-new"},
		{Hash: "h-archive-only"},
	}

	changesOverview := make(PublishedBuildChangesOverview)
	got := validateComparisonInternalDocuments(oldDocs, newDocs, newDocData, comparisonIdByKey, &changesOverview)

	want := map[string]interface{}{
		"comparison_internal_document": map[string]interface{}{
			"cmp-main:changed": map[string]interface{}{
				"Hash": map[string]interface{}{"old": "h-old", "new": "h-new"},
			},
			"cmp-main:db-only":     "comparison internal document not found in build archive",
			"cmp-new:archive-only": "unexpected comparison internal document (not found in database)",
		},
		"comparison_internal_document_data": map[string]interface{}{
			"h-old":          "comparison internal document data not found in build archive",
			"h-db-only":      "comparison internal document data not found in build archive",
			"h-new":          "unexpected comparison internal document data (not found in database)",
			"h-archive-only": "unexpected comparison internal document data (not found in database)",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("reconcileComparisonInternalDocuments() = %#v, want %#v", got, want)
	}
}

func TestReconcileComparisonInternalDocumentsEmptyArchive(t *testing.T) {
	mainComparison := view.ComparisonKey{PackageId: "pkg", Version: "v2", Revision: 1, PreviousVersionPackageId: "pkg", PreviousVersion: "v1", PreviousVersionRevision: 1}
	comparisonIdByKey := makeComparisonInternalDocumentValidationScope(
		[]entity.VersionComparisonEntity{{
			PackageId:         mainComparison.PackageId,
			Version:           mainComparison.Version,
			Revision:          mainComparison.Revision,
			PreviousPackageId: mainComparison.PreviousVersionPackageId,
			PreviousVersion:   mainComparison.PreviousVersion,
			PreviousRevision:  mainComparison.PreviousVersionRevision,
			ComparisonId:      "cmp-main",
		}},
		nil,
		nil,
	)

	oldDocs := []entity.ComparisonInternalDocumentEntity{
		comparisonInternalDocument(mainComparison, "orphan", "f-orphan", "h-orphan"),
	}

	changesOverview := make(PublishedBuildChangesOverview)
	got := validateComparisonInternalDocuments(oldDocs, nil, nil, comparisonIdByKey, &changesOverview)

	want := map[string]interface{}{
		"comparison_internal_document": map[string]interface{}{
			"cmp-main:orphan": "comparison internal document not found in build archive",
		},
		"comparison_internal_document_data": map[string]interface{}{
			"h-orphan": "comparison internal document data not found in build archive",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("reconcileComparisonInternalDocuments() = %#v, want %#v", got, want)
	}
}
