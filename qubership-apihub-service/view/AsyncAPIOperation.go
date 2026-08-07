package view

const (
	SendAction    string = "send"
	ReceiveAction string = "receive"
)

func ValidAsyncAPIAction(actionValue string) bool {
	switch actionValue {
	case SendAction, ReceiveAction:
		return true
	}
	return false
}

type AsyncAPIOperationMetadata struct {
	Action           string   `json:"action"`
	Channel          string   `json:"channel"`
	Protocol         string   `json:"protocol"`
	AsyncOperationId string   `json:"asyncOperationId"`
	MessageId        string   `json:"messageId"`
	Tags             []string `json:"tags,omitempty"`
	// Address is the channel's address - what a consumer binds to, as opposed to Channel above,
	// which is a display title. PayloadIdentity is the declaration path of the message's payload
	// schema. The builder pairs operations across versions on the two of them together, which is
	// what lets a version survive an id whose generated suffix changed.
	//
	// Both omitempty: the builder omits them for a channel with no address and for a message with
	// an inline payload, and a client must be able to tell that apart from an empty value.
	Address         string `json:"address,omitempty"`
	PayloadIdentity string `json:"payloadIdentity,omitempty"`
}

type AsyncAPIOperationSingleView struct {
	SingleOperationView
	AsyncAPIOperationMetadata
}

type AsyncAPIOperationView struct {
	OperationListView
	AsyncAPIOperationMetadata
}

type DeprecatedAsyncAPIOperationView struct {
	DeprecatedOperationView
	AsyncAPIOperationMetadata
}

type AsyncAPIOperationComparisonChangelogView struct {
	GenericComparisonOperationView
	AsyncAPIOperationMetadata
}

type AsyncAPIOperationComparisonChangesView struct {
	OperationComparisonChangesView
	AsyncAPIOperationMetadata
}

type AsyncAPIOperationPairChangesView struct {
	CurrentOperation             *AsyncAPIOperationComparisonChangelogView `json:"currentOperation,omitempty"`
	PreviousOperation            *AsyncAPIOperationComparisonChangelogView `json:"previousOperation,omitempty"`
	ChangeSummary                ChangeSummary                             `json:"changeSummary"`
	ComparisonInternalDocumentId string                                    `json:"comparisonInternalDocumentId"`
}
