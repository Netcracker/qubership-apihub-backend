package view

type PackageVisibilityRoots struct {
	WorkspaceId    string   `json:"workspaceId"`
	VisibleRoots   []string `json:"visibleRoots"`
	InvisibleRoots []string `json:"invisibleRoots"`
}
