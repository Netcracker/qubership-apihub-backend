package entity

import (
	"testing"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
	"github.com/stretchr/testify/assert"
)

func versionComparison(operationTypes []view.OperationType, contractTypes []view.ContractType, refs []string) VersionComparisonEntity {
	return VersionComparisonEntity{
		ComparisonId:   "comparison-1",
		OperationTypes: operationTypes,
		ContractTypes:  contractTypes,
		Refs:           refs,
		Metadata:       Metadata{},
	}
}

func TestVersionComparisonGetChanges(t *testing.T) {
	restBreaking := []view.OperationType{{ApiType: "rest", ChangesSummary: view.ChangeSummary{Breaking: 1}}}
	restNonBreaking := []view.OperationType{{ApiType: "rest", ChangesSummary: view.ChangeSummary{NonBreaking: 1}}}
	ddlBreaking := []view.ContractType{{ContractType: view.ContractTypeDdl, ChangesSummary: view.ChangeSummary{Breaking: 2}}}
	ddlNonBreaking := []view.ContractType{{ContractType: view.ContractTypeDdl, ChangesSummary: view.ChangeSummary{NonBreaking: 2}}}

	stored := versionComparison(restBreaking, ddlBreaking, []string{"ref-1"})

	tests := []struct {
		name                      string
		rebuilt                   VersionComparisonEntity
		operationChangesFromCache bool
		ddlChangesFromCache       bool
		expectedChangeKeys        []string
	}{
		{
			name:               "both kinds of changes rebuilt and equal",
			rebuilt:            versionComparison(restBreaking, ddlBreaking, []string{"ref-1"}),
			expectedChangeKeys: []string{},
		},
		{
			name:               "both kinds of changes rebuilt and both summaries changed",
			rebuilt:            versionComparison(restNonBreaking, ddlNonBreaking, []string{"ref-1"}),
			expectedChangeKeys: []string{"rest", view.ContractTypeDdl},
		},
		{
			name:               "contract type missing from the build archive",
			rebuilt:            versionComparison(restBreaking, nil, []string{"ref-1"}),
			expectedChangeKeys: []string{view.ContractTypeDdl},
		},
		{
			name:                      "operation changes taken from cache",
			rebuilt:                   versionComparison(nil, ddlNonBreaking, []string{"ref-1"}),
			operationChangesFromCache: true,
			expectedChangeKeys:        []string{view.ContractTypeDdl},
		},
		{
			name:                "ddl changes taken from cache",
			rebuilt:             versionComparison(restNonBreaking, nil, []string{"ref-1"}),
			ddlChangesFromCache: true,
			expectedChangeKeys:  []string{"rest"},
		},
		{
			// mergeVersionComparisons assembles refs from both indexes, so they are complete even when
			// one kind of changes came from cache.
			name:                      "refs are compared even when one kind of changes came from cache",
			rebuilt:                   versionComparison(nil, ddlBreaking, []string{"ref-2"}),
			operationChangesFromCache: true,
			expectedChangeKeys:        []string{"Refs"},
		},
		{
			name:               "refs of a fully rebuilt comparison are compared",
			rebuilt:            versionComparison(restBreaking, ddlBreaking, []string{"ref-2"}),
			expectedChangeKeys: []string{"Refs"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changes := stored.GetChanges(test.rebuilt, test.operationChangesFromCache, test.ddlChangesFromCache)
			actualKeys := make([]string, 0, len(changes))
			for key := range changes {
				actualKeys = append(actualKeys, key)
			}
			assert.ElementsMatch(t, test.expectedChangeKeys, actualKeys)
		})
	}
}
