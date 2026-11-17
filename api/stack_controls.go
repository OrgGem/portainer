package portainer

// StackAction is an independently grantable operation on a Compose stack.
type StackAction string

const (
	StackView         StackAction = "view"
	StackConfigUpload StackAction = "config.upload"
	StackConfigApply  StackAction = "config.apply"
	StackDeploy       StackAction = "deploy"
	StackEdit         StackAction = "edit"
)

type StackAccessGrant struct {
	UserID  UserID        `json:"UserID,omitempty"`
	TeamID  TeamID        `json:"TeamID,omitempty"`
	Actions []StackAction `json:"Actions"`
}

// A non-nil policy opts a stack into deny-by-default operation authorization.
// Existing environment and resource ownership checks still apply.
type StackAccessPolicy struct {
	Version int                `json:"Version"`
	Grants  []StackAccessGrant `json:"Grants"`
}

type StackConfigTarget struct {
	ID             string `json:"ID"`
	Service        string `json:"Service"`
	Target         string `json:"Target"`
	ActiveRevision string `json:"ActiveRevision,omitempty"`
}

type StackConfigRevision struct {
	ID        string `json:"ID"`
	TargetID  string `json:"TargetID"`
	Filename  string `json:"Filename"`
	SHA256    string `json:"SHA256"`
	Size      int    `json:"Size"`
	CreatedAt int64  `json:"CreatedAt"`
	CreatedBy UserID `json:"CreatedBy"`
	AppliedAt int64  `json:"AppliedAt,omitempty"`
	AppliedBy UserID `json:"AppliedBy,omitempty"`
}

type StackConfigEvent struct {
	RevisionID string `json:"RevisionID"`
	Action     string `json:"Action"`
	UserID     UserID `json:"UserID"`
	Time       int64  `json:"Time"`
	Success    bool   `json:"Success"`
}

// Contents live in immutable files outside the Compose project. API responses
// contain metadata only; contents require the dedicated authorized endpoint.
type StackConfiguration struct {
	Version   int                   `json:"Version"`
	Targets   []StackConfigTarget   `json:"Targets"`
	Revisions []StackConfigRevision `json:"Revisions"`
	Events    []StackConfigEvent    `json:"Events"`
	// PendingRevision survives an interrupted deployment and requires recovery.
	PendingRevision string            `json:"PendingRevision,omitempty"`
	PendingImages   map[string]string `json:"PendingImages,omitempty"`
}
