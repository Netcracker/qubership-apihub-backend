package archive

import (
	"archive/zip"
	"bytes"
	"testing"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/entity"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
)

const (
	mainComparisonFileId        = "main-comparison"
	dashboardComparisonFileId   = "dashboard-comparison"
	packageComparisonFileId     = "package-comparison"
	mainComparisonDocumentId    = "main-document"
	dashboardComparisonDocId    = "dashboard-document"
	packageComparisonDocumentId = "package-document"
)

func TestReadComparisonInternalDocumentsToEntitiesSkipsCachedComparisons(t *testing.T) {
	dashboardComparisonId := makeComparisonId("dashboard1", "v2", 1, "dashboard1", "v1", 1)
	packageComparisonId := makeComparisonId("package", "v2", 1, "package", "v1", 1)

	tests := []struct {
		name            string
		configure       func(*BuildResultToEntitiesReader) []entity.VersionComparisonEntity
		wantDocumentIds []string
	}{
		{
			name: "comparison stored with the same builder version",
			configure: func(_ *BuildResultToEntitiesReader) []entity.VersionComparisonEntity {
				return []entity.VersionComparisonEntity{{
					ComparisonId:   dashboardComparisonId,
					BuilderVersion: "3.0.0",
					OperationTypes: []view.OperationType{},
				}}
			},
			wantDocumentIds: []string{mainComparisonDocumentId, packageComparisonDocumentId},
		},
		{
			name: "comparison marked as from cache",
			configure: func(reader *BuildResultToEntitiesReader) []entity.VersionComparisonEntity {
				reader.PackageComparisons.Comparisons[2].FromCache = true
				return []entity.VersionComparisonEntity{{
					ComparisonId:   packageComparisonId,
					BuilderVersion: "3.0.0",
					OperationTypes: []view.OperationType{},
				}}
			},
			wantDocumentIds: []string{mainComparisonDocumentId, dashboardComparisonDocId},
		},
		{
			name: "operation comparison cached while ddl comparison is rebuilt",
			configure: func(reader *BuildResultToEntitiesReader) []entity.VersionComparisonEntity {
				reader.PackageDdlComparisons.Comparisons = []view.DdlVersionComparison{{
					ComparisonFileId:         dashboardComparisonFileId,
					PackageId:                "dashboard1",
					Version:                  "v2",
					Revision:                 1,
					PreviousVersionPackageId: "dashboard1",
					PreviousVersion:          "v1",
					PreviousVersionRevision:  1,
				}}
				return []entity.VersionComparisonEntity{{
					ComparisonId:   dashboardComparisonId,
					BuilderVersion: "3.0.0",
					OperationTypes: []view.OperationType{},
				}}
			},
			wantDocumentIds: []string{
				mainComparisonDocumentId,
				dashboardComparisonDocId,
				packageComparisonDocumentId,
			},
		},
	}

	currentVersionRefs := []*entity.PublishedReferenceEntity{
		{RefPackageId: "dashboard1", RefVersion: "v2", RefRevision: 1},
		{
			RefPackageId:       "package",
			RefVersion:         "v2",
			RefRevision:        1,
			ParentRefPackageId: "dashboard1",
			ParentRefVersion:   "v2",
			ParentRefRevision:  1,
		},
	}
	previousVersionRefs := []entity.PublishedReferenceEntity{
		{RefPackageId: "dashboard1", RefVersion: "v1", RefRevision: 1},
		{
			RefPackageId:       "package",
			RefVersion:         "v1",
			RefRevision:        1,
			ParentRefPackageId: "dashboard1",
			ParentRefVersion:   "v1",
			ParentRefRevision:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := dashboardChainReader()
			addComparisonInternalDocuments(t, reader)
			storedComparisons := tt.configure(reader)
			repo := &fakePublishedRepo{
				storedComparisons: storedComparisons,
				refsByVersion: map[VersionKey][]entity.PublishedReferenceEntity{
					versionKey("dashboard2", "v1", 1): previousVersionRefs,
				},
			}

			resolver, err := reader.ResolveComparisonRefs(currentVersionRefs, repo)
			if err != nil {
				t.Fatalf("ResolveComparisonRefs() error = %v", err)
			}
			_, _, comparisonFileIdToKeyMap, err := reader.ReadOperationComparisonsToEntities(nil, nil, resolver)
			if err != nil {
				t.Fatalf("ReadOperationComparisonsToEntities() error = %v", err)
			}
			_, _, ddlComparisonFileIdToKeyMap, err := reader.ReadDdlContractComparisonsToEntities(nil, nil, resolver)
			if err != nil {
				t.Fatalf("ReadDdlContractComparisonsToEntities() error = %v", err)
			}
			for fileId, key := range ddlComparisonFileIdToKeyMap {
				comparisonFileIdToKeyMap[fileId] = key
			}

			documents, data, err := reader.ReadComparisonInternalDocumentsToEntities(
				comparisonFileIdToKeyMap,
				resolver.SkippedVersionComparisonIds(),
			)
			if err != nil {
				t.Fatalf("ReadComparisonInternalDocumentsToEntities() error = %v", err)
			}

			gotDocumentIds := make([]string, 0, len(documents))
			for _, document := range documents {
				gotDocumentIds = append(gotDocumentIds, document.DocumentId)
			}
			if !sameElements(gotDocumentIds, tt.wantDocumentIds) {
				t.Errorf("comparison internal document ids = %v, want %v", gotDocumentIds, tt.wantDocumentIds)
			}

			gotData := make([]string, 0, len(data))
			for _, documentData := range data {
				gotData = append(gotData, string(documentData.Data))
			}
			wantData := make([]string, 0, len(tt.wantDocumentIds))
			for _, documentId := range tt.wantDocumentIds {
				wantData = append(wantData, documentId+"-data")
			}
			if !sameElements(gotData, wantData) {
				t.Errorf("comparison internal document data = %v, want %v", gotData, wantData)
			}
		})
	}
}

func addComparisonInternalDocuments(t *testing.T, reader *BuildResultToEntitiesReader) {
	t.Helper()
	reader.PackageComparisons.Comparisons[0].ComparisonFileId = mainComparisonFileId
	reader.PackageComparisons.Comparisons[1].ComparisonFileId = dashboardComparisonFileId
	reader.PackageComparisons.Comparisons[2].ComparisonFileId = packageComparisonFileId

	documents := []view.ComparisonInternalDocument{
		{
			InternalDocument: view.InternalDocument{Id: mainComparisonDocumentId, Filename: mainComparisonDocumentId + ".json"},
			ComparisonFileId: mainComparisonFileId,
		},
		{
			InternalDocument: view.InternalDocument{Id: dashboardComparisonDocId, Filename: dashboardComparisonDocId + ".json"},
			ComparisonFileId: dashboardComparisonFileId,
		},
		{
			InternalDocument: view.InternalDocument{Id: packageComparisonDocumentId, Filename: packageComparisonDocumentId + ".json"},
			ComparisonFileId: packageComparisonFileId,
		},
	}
	reader.ComparisonInternalDocuments.Documents = documents
	reader.ComparisonInternalDocumentsHeaders = make(map[string]*zip.File, len(documents))
	for _, document := range documents {
		reader.ComparisonInternalDocumentsHeaders[document.Filename] = newComparisonInternalDocumentZipFile(
			t,
			document.Filename,
			[]byte(document.Id+"-data"),
		)
	}
}

func newComparisonInternalDocumentZipFile(t *testing.T, filename string, data []byte) *zip.File {
	t.Helper()
	buffer := bytes.NewBuffer(nil)
	writer := zip.NewWriter(buffer)
	file, err := writer.Create(filename)
	if err != nil {
		t.Fatalf("failed to create zip file %q: %v", filename, err)
	}
	if _, err = file.Write(data); err != nil {
		t.Fatalf("failed to write zip file %q: %v", filename, err)
	}
	if err = writer.Close(); err != nil {
		t.Fatalf("failed to close zip file %q: %v", filename, err)
	}
	reader, err := zip.NewReader(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
	if err != nil {
		t.Fatalf("failed to read zip file %q: %v", filename, err)
	}
	return reader.File[0]
}
