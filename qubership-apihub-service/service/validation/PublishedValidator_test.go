package validation

import (
	"testing"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/archive"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
)

const (
	restDocumentSlug  = "openapi-yaml"
	asyncDocumentSlug = "events-yaml"

	comparisonPackageId = "QS.PKG"
	comparisonVersion   = "2026.1"
	comparisonRevision  = 3
	previousVersionName = "2025.4"
)

func buildArchive(versionHasErrors bool, documents []view.PackageDocument, notifications []view.BuilderNotification) *archive.BuildResultArchive {
	return &archive.BuildResultArchive{
		PackageInfo:        view.PackageInfoFile{HasErrors: versionHasErrors},
		PackageDocuments:   view.PackageDocumentsFile{Documents: documents},
		BuildNotifications: view.BuildNotificationsFile{Notifications: notifications},
	}
}

func errorNotification(documentId string) view.BuilderNotification {
	return view.BuilderNotification{
		Severity:   view.BuilderNotificationSeverityError,
		Category:   "document-build",
		Message:    "Cannot process the document",
		DocumentId: documentId,
	}
}

func TestValidateErroredVersionNotPublishedAsRelease(t *testing.T) {
	const packageId = "QS.PKG"
	const versionName = "2026.1"

	tests := []struct {
		name                string
		status              view.VersionStatus
		hasErrors           bool
		comparisonHasErrors bool
		ddlComparisonErrors bool
		migrationBuild      bool
		expectError         bool
	}{
		{
			name:        "release status with errors is refused",
			status:      view.Release,
			hasErrors:   true,
			expectError: true,
		},
		{
			name:        "release status without errors is accepted",
			status:      view.Release,
			hasErrors:   false,
			expectError: false,
		},
		{
			name:        "draft status with errors is accepted",
			status:      view.Draft,
			hasErrors:   true,
			expectError: false,
		},
		{
			// A release that declares a previousVersion is expected to ship a reliable changelog, so an error
			// in the comparison blocks it even when every document built cleanly.
			name:                "release status with an errored changelog is refused",
			status:              view.Release,
			comparisonHasErrors: true,
			expectError:         true,
		},
		{
			name:                "release status with an errored DDL changelog is refused",
			status:              view.Release,
			ddlComparisonErrors: true,
			expectError:         true,
		},
		{
			// Comparison errors never block a draft; they mark the comparison, not the version.
			name:                "draft status with an errored changelog is accepted",
			status:              view.Draft,
			comparisonHasErrors: true,
			expectError:         false,
		},
		{
			name:           "migration build in release status with errors is refused",
			status:         view.Release,
			hasErrors:      true,
			migrationBuild: true,
			expectError:    true,
		},
	}

	validator := NewPublishedValidator(nil)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buildArc := &archive.BuildResultArchive{
				PackageInfo: view.PackageInfoFile{
					PackageId:      packageId,
					Version:        versionName,
					Status:         string(tt.status),
					MigrationBuild: tt.migrationBuild,
					HasErrors:      tt.hasErrors,
				},
				PackageComparisons: view.PackageComparisonsFile{
					Comparisons: []view.VersionComparison{{HasErrors: tt.comparisonHasErrors}},
				},
				PackageDdlComparisons: view.PackageDdlComparisonsFile{
					Comparisons: []view.DdlVersionComparison{{HasErrors: tt.ddlComparisonErrors}},
				},
			}

			err := validator.ValidateErroredVersionNotPublishedAsRelease(buildArc)
			if tt.expectError && err == nil {
				t.Fatalf("expected the build result to be rejected, but it was accepted")
			}
			if !tt.expectError && err != nil {
				t.Fatalf("expected the build result to be accepted, but it was rejected: %v", err)
			}
		})
	}
}

func TestValidateBuildNotifications(t *testing.T) {
	validDocument := view.PackageDocument{Slug: restDocumentSlug}
	erroredDocument := view.PackageDocument{Slug: asyncDocumentSlug, HasErrors: true}

	tests := []struct {
		name        string
		buildArc    *archive.BuildResultArchive
		expectError bool
	}{
		{
			name: "flags and notifications are stored as they come",
			buildArc: buildArchive(true,
				[]view.PackageDocument{validDocument, erroredDocument},
				[]view.BuilderNotification{errorNotification(asyncDocumentSlug)}),
			expectError: false,
		},
		{
			name:        "no errors at all",
			buildArc:    buildArchive(false, []view.PackageDocument{validDocument}, nil),
			expectError: false,
		},
		{
			// The message concerns the previous version of the comparison, whose documents are not part of
			// this build result.
			name: "error notification for a document absent from the build result",
			buildArc: buildArchive(true,
				[]view.PackageDocument{validDocument},
				[]view.BuilderNotification{errorNotification("removed-in-this-version-yaml")}),
			expectError: false,
		},
		{
			name: "severity outside the contract",
			buildArc: buildArchive(false,
				[]view.PackageDocument{validDocument},
				[]view.BuilderNotification{{Severity: view.BuilderNotificationSeverity(7), Message: "Cannot process the document"}}),
			expectError: true,
		},
		{
			name: "notification without a message",
			buildArc: buildArchive(false,
				[]view.PackageDocument{validDocument},
				[]view.BuilderNotification{{Severity: view.BuilderNotificationSeverityError, Category: "build-document"}}),
			expectError: true,
		},
	}

	validator := NewPublishedValidator(nil)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateBuildNotifications(tt.buildArc)
			if tt.expectError && err == nil {
				t.Fatalf("expected the build result to be rejected, but it was accepted")
			}
			if !tt.expectError && err != nil {
				t.Fatalf("expected the build result to be accepted, but it was rejected: %v", err)
			}
		})
	}
}

func comparisonNotifications(previousVersion string, notifications ...view.BuilderNotification) view.ComparisonNotifications {
	return view.ComparisonNotifications{
		PackageId:                comparisonPackageId,
		Version:                  comparisonVersion,
		Revision:                 comparisonRevision,
		PreviousVersionPackageId: comparisonPackageId,
		PreviousVersion:          previousVersion,
		PreviousVersionRevision:  1,
		Notifications:            notifications,
	}
}

func builtComparison(previousVersion string) view.VersionComparison {
	return view.VersionComparison{
		PackageId:                comparisonPackageId,
		Version:                  comparisonVersion,
		Revision:                 comparisonRevision,
		PreviousVersionPackageId: comparisonPackageId,
		PreviousVersion:          previousVersion,
		PreviousVersionRevision:  1,
	}
}

func TestValidateComparisonNotifications(t *testing.T) {
	tests := []struct {
		name        string
		buildArc    *archive.BuildResultArchive
		expectError bool
	}{
		{
			name: "every entry names a comparison of this build",
			buildArc: &archive.BuildResultArchive{
				PackageComparisons: view.PackageComparisonsFile{
					Comparisons: []view.VersionComparison{builtComparison(previousVersionName)},
				},
				ComparisonNotifications: view.ComparisonNotificationsFile{
					Comparisons: []view.ComparisonNotifications{
						comparisonNotifications(previousVersionName, errorNotification("")),
					},
				},
			},
			expectError: false,
		},
		{
			// A comparison the build recalculated cleanly ships an empty entry, which clears the rows stored
			// for it earlier.
			name: "entry with no notifications",
			buildArc: &archive.BuildResultArchive{
				PackageComparisons: view.PackageComparisonsFile{
					Comparisons: []view.VersionComparison{builtComparison(previousVersionName)},
				},
				ComparisonNotifications: view.ComparisonNotificationsFile{
					Comparisons: []view.ComparisonNotifications{comparisonNotifications(previousVersionName)},
				},
			},
			expectError: false,
		},
		{
			name:        "no comparison notifications at all",
			buildArc:    &archive.BuildResultArchive{},
			expectError: false,
		},
		{
			// The pair is matched against ddl-comparisons.json too, so a DDL-only changelog is accepted.
			name: "entry names a DDL comparison of this build",
			buildArc: &archive.BuildResultArchive{
				PackageDdlComparisons: view.PackageDdlComparisonsFile{
					Comparisons: []view.DdlVersionComparison{{
						PackageId:                comparisonPackageId,
						Version:                  comparisonVersion,
						Revision:                 comparisonRevision,
						PreviousVersionPackageId: comparisonPackageId,
						PreviousVersion:          previousVersionName,
						PreviousVersionRevision:  1,
					}},
				},
				ComparisonNotifications: view.ComparisonNotificationsFile{
					Comparisons: []view.ComparisonNotifications{
						comparisonNotifications(previousVersionName, errorNotification("")),
					},
				},
			},
			expectError: false,
		},
		{
			// A reused comparison creates no version_comparison row here, so an entry for it would clear the
			// rows recorded when it was really calculated.
			name: "entry names a comparison the build reused from cache",
			buildArc: &archive.BuildResultArchive{
				PackageComparisons: view.PackageComparisonsFile{
					Comparisons: []view.VersionComparison{{
						PackageId:                comparisonPackageId,
						Version:                  comparisonVersion,
						Revision:                 comparisonRevision,
						PreviousVersionPackageId: comparisonPackageId,
						PreviousVersion:          previousVersionName,
						PreviousVersionRevision:  1,
						FromCache:                true,
					}},
				},
				ComparisonNotifications: view.ComparisonNotificationsFile{
					Comparisons: []view.ComparisonNotifications{
						comparisonNotifications(previousVersionName, errorNotification("")),
					},
				},
			},
			expectError: true,
		},
		{
			// Without a matching comparison there is no version_comparison row to store the messages against,
			// so the publish fails instead of dropping them.
			name: "entry names a pair the build did not compare",
			buildArc: &archive.BuildResultArchive{
				PackageComparisons: view.PackageComparisonsFile{
					Comparisons: []view.VersionComparison{builtComparison(previousVersionName)},
				},
				ComparisonNotifications: view.ComparisonNotificationsFile{
					Comparisons: []view.ComparisonNotifications{
						comparisonNotifications("2025.3", errorNotification("")),
					},
				},
			},
			expectError: true,
		},
		{
			name: "notification without a message",
			buildArc: &archive.BuildResultArchive{
				PackageComparisons: view.PackageComparisonsFile{
					Comparisons: []view.VersionComparison{builtComparison(previousVersionName)},
				},
				ComparisonNotifications: view.ComparisonNotificationsFile{
					Comparisons: []view.ComparisonNotifications{
						comparisonNotifications(previousVersionName,
							view.BuilderNotification{Severity: view.BuilderNotificationSeverityError}),
					},
				},
			},
			expectError: true,
		},
	}

	validator := NewPublishedValidator(nil)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateComparisonNotifications(tt.buildArc)
			if tt.expectError && err == nil {
				t.Fatalf("expected the build result to be rejected, but it was accepted")
			}
			if !tt.expectError && err != nil {
				t.Fatalf("expected the build result to be accepted, but it was rejected: %v", err)
			}
		})
	}
}
