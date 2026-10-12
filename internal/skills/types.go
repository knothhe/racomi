package skills

type Skill struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	Source          string `json:"source"`
	Ref             string `json:"ref"`
	Subpath         string `json:"subpath"`
	Revision        string `json:"revision"`
	ContentHash     string `json:"contentHash"`
	InstalledAt     string `json:"installedAt"`
	UpdatedAt       string `json:"updatedAt"`
	SourceID        string `json:"sourceId"`
	UpstreamRemoved bool   `json:"upstreamRemoved"`
}

// Source tracks a repository independently from the skills installed from it.
type Source struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Repository  string        `json:"repository"`
	Ref         string        `json:"ref"`
	Subpath     string        `json:"subpath"`
	Mode        string        `json:"mode"`
	Revision    string        `json:"revision"`
	RefreshedAt string        `json:"refreshedAt"`
	LastError   string        `json:"lastError"`
	Warnings    []string      `json:"warnings,omitempty"`
	Catalog     []SourceSkill `json:"catalog"`
}

type SourceSkill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Subpath     string `json:"subpath"`
	Revision    string `json:"revision"`
	ContentHash string `json:"contentHash"`
	Removed     bool   `json:"removed"`
	New         bool   `json:"new"`
	SkipReason  string `json:"skipReason,omitempty"`
}

type Preset struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Icon        string   `json:"icon,omitempty"`
	SkillIDs    []string `json:"skillIds"`
}

type ProjectDirectory struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

type Settings struct {
	Theme                     string             `json:"theme,omitempty"`
	Locale                    string             `json:"locale,omitempty"`
	NavLayout                 string             `json:"navLayout,omitempty"`
	DeploymentMode            string             `json:"deploymentMode"`
	DefaultProjectDirectories []string           `json:"defaultProjectDirectories"`
	DefaultGlobalDirectories  []string           `json:"defaultGlobalDirectories"`
	CustomProjectDirectories  []ProjectDirectory `json:"customProjectDirectories"`
}

type Deployment struct {
	Path        string `json:"path"`
	Mode        string `json:"mode"`
	SkillID     string `json:"skillId"`
	Name        string `json:"name"`
	ContentHash string `json:"contentHash"`
}

type Target struct {
	Paths            []string     `json:"paths"`
	ProjectRoot      string       `json:"projectRoot"`
	DirectoryIDs     []string     `json:"directoryIds"`
	CustomPaths      []string     `json:"customPaths"`
	ID               string       `json:"id"`
	Name             string       `json:"name"`
	Icon             string       `json:"icon,omitempty"`
	Kind             string       `json:"kind"`
	Path             string       `json:"path"`
	EnabledSkillIDs  []string     `json:"enabledSkillIds"`
	EnabledPresetIDs []string     `json:"enabledPresetIds"`
	Deployments      []Deployment `json:"deployments"`
	Pending          bool         `json:"pending"`
}

type State struct {
	Settings Settings `json:"settings"`
	Schema   int      `json:"schema"`
	Skills   []Skill  `json:"skills"`
	Presets  []Preset `json:"presets"`
	Targets  []Target `json:"targets"`
	Sources  []Source `json:"sources"`
}

type Candidate struct {
	Skill Skill  `json:"skill"`
	Path  string `json:"-"`
}

type SnapshotInfo struct {
	ID          string `json:"id"`
	CreatedAt   string `json:"createdAt"`
	Reason      string `json:"reason"`
	SkillCount  int    `json:"skillCount"`
	PresetCount int    `json:"presetCount"`
}

type DirectoryPlan struct {
	TargetID          string   `json:"targetId"`
	Path              string   `json:"path"`
	Add               []string `json:"add"`
	Update            []string `json:"update"`
	Remove            []string `json:"remove"`
	Conflicts         []string `json:"conflicts"`
	BlockingConflicts []string `json:"blockingConflicts"`
	// Overwrite lists existing absolute paths that only a force apply may replace
	// or remove. Symlinks refer to the link itself, never to their destination.
	Overwrite []string `json:"overwrite"`
	// Fingerprint is opaque. Reviewed force applies and staging rechecks require
	// the same filesystem content, including the current overwritten content.
	Fingerprint string `json:"fingerprint"`
}

type ApplyPlan struct {
	Directories       []DirectoryPlan `json:"directories"`
	TargetID          string          `json:"targetId"`
	Add               []string        `json:"add"`
	Update            []string        `json:"update"`
	Remove            []string        `json:"remove"`
	Conflicts         []string        `json:"conflicts"`
	BlockingConflicts []string        `json:"blockingConflicts"`
	Overwrite         []string        `json:"overwrite"`
}
