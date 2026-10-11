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
	SkillIDs    []string `json:"skillIds"`
}

type ProjectDirectory struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

type Settings struct {
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
	TargetID  string   `json:"targetId"`
	Path      string   `json:"path"`
	Add       []string `json:"add"`
	Update    []string `json:"update"`
	Remove    []string `json:"remove"`
	Conflicts []string `json:"conflicts"`
}

type ApplyPlan struct {
	Directories []DirectoryPlan `json:"directories"`
	TargetID    string          `json:"targetId"`
	Add         []string        `json:"add"`
	Update      []string        `json:"update"`
	Remove      []string        `json:"remove"`
	Conflicts   []string        `json:"conflicts"`
}
