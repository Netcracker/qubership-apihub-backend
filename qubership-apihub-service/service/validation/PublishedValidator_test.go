package validation

import (
	"testing"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/archive"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/exception"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
	"github.com/stretchr/testify/assert"
)

func validDdlContract() view.PackageDdlContract {
	return view.PackageDdlContract{
		DdlEntityId:               "public-table-users",
		Kind:                      view.DdlEntityKindTable,
		Name:                      "users",
		SchemaName:                "public",
		DocumentId:                "shop.sql",
		VersionInternalDocumentId: "shop",
	}
}

func validMcpContract() view.PackageMcpContract {
	return view.PackageMcpContract{
		McpEntityId: "mcp-tool-get_forecast",
		Kind:        view.McpEntityKindTool,
		Title:       "get_forecast",
		McpEndpoint: "/mcp",
		DocumentId:  "tools-forecast-json",
	}
}

func TestValidatePackageDdlContracts(t *testing.T) {
	tests := []struct {
		name       string
		tables     []view.PackageDdlContract
		comparison []view.DdlVersionComparison
		wantErr    bool
	}{
		{
			name:    "valid ddl contract",
			tables:  []view.PackageDdlContract{validDdlContract()},
			wantErr: false,
		},
		{
			name:    "no ddl contracts is valid",
			tables:  nil,
			wantErr: false,
		},
		{
			name: "missing required field",
			tables: []view.PackageDdlContract{
				func() view.PackageDdlContract {
					c := validDdlContract()
					c.SchemaName = ""
					return c
				}(),
			},
			wantErr: true,
		},
		{
			name: "unsupported kind",
			tables: []view.PackageDdlContract{
				func() view.PackageDdlContract {
					c := validDdlContract()
					c.Kind = "view"
					return c
				}(),
			},
			wantErr: true,
		},
	}

	p := publishedValidatorImpl{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buildArc := &archive.BuildResultArchive{
				PackageDdlContracts:   view.PackageDdlContractsFile{Tables: tt.tables},
				PackageDdlComparisons: view.PackageDdlComparisonsFile{Comparisons: tt.comparison},
			}
			err := p.validatePackageDdlContracts(buildArc, &view.BuildConfig{})
			if tt.wantErr {
				assert.Error(t, err)
				customErr, ok := err.(*exception.CustomError)
				assert.True(t, ok, "expected *exception.CustomError, got %T", err)
				assert.Equal(t, exception.InvalidPackagedFile, customErr.Code)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidatePackageMcpContracts(t *testing.T) {
	tests := []struct {
		name    string
		mcp     view.PackageMcpContractsFile
		wantErr bool
	}{
		{
			name:    "valid mcp contract",
			mcp:     view.PackageMcpContractsFile{Tools: []view.PackageMcpContract{validMcpContract()}},
			wantErr: false,
		},
		{
			name:    "no mcp contracts is valid",
			mcp:     view.PackageMcpContractsFile{},
			wantErr: false,
		},
		{
			name: "missing required field",
			mcp: view.PackageMcpContractsFile{
				Tools: []view.PackageMcpContract{
					func() view.PackageMcpContract {
						c := validMcpContract()
						c.McpEndpoint = ""
						return c
					}(),
				},
			},
			wantErr: true,
		},
		{
			name: "unsupported kind",
			mcp: view.PackageMcpContractsFile{
				Prompts: []view.PackageMcpContract{
					func() view.PackageMcpContract {
						c := validMcpContract()
						c.Kind = "unknown"
						return c
					}(),
				},
			},
			wantErr: true,
		},
	}

	p := publishedValidatorImpl{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buildArc := &archive.BuildResultArchive{PackageMcpContracts: tt.mcp}
			err := p.validatePackageMcpContracts(buildArc, &view.BuildConfig{})
			if tt.wantErr {
				assert.Error(t, err)
				customErr, ok := err.(*exception.CustomError)
				assert.True(t, ok, "expected *exception.CustomError, got %T", err)
				assert.Equal(t, exception.InvalidPackagedFile, customErr.Code)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
