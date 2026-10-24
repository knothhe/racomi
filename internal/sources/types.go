// Package sources discovers skills in Git repositories and the public skills.sh index.
// Repository content is read as data; hooks, filters, and skill scripts are never run.
package sources

type SearchResult struct {
	Name              string `json:"name"`
	Slug              string `json:"slug"`
	Source            string `json:"source"`
	Installs          int    `json:"installs"`
	URL               string `json:"url"`
	Supported         bool   `json:"supported"`
	UnsupportedReason string `json:"unsupportedReason"`
}

type SkillListPage struct {
	Skills  []SearchResult `json:"skills"`
	Total   int            `json:"total"`
	HasMore bool           `json:"hasMore"`
	Page    int            `json:"page"`
}

type HubAvailability struct {
	URL       string `json:"url"`
	State     string `json:"state"`
	Revision  string `json:"revision"`
	CheckedAt string `json:"checkedAt"`
	Reason    string `json:"reason"`
}

type CandidateInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Subpath     string          `json:"subpath"`
	Revision    string          `json:"revision"`
	ContentHash string          `json:"contentHash"`
	Variants    []CandidateInfo `json:"variants,omitempty"`
}

// SkippedSkill identifies skill content that could not be safely read. Its
// previous catalog entry must be retained instead of treated as removed.
type SkippedSkill struct {
	Subpath        string `json:"subpath"`
	Reason         string `json:"reason"`
	blockedSubtree bool   // A failed walk can hide previously known descendants.
	name           string // Trusted metadata can survive an asset or budget skip.
}

type Preview struct {
	ID         string          `json:"id"`
	Local      bool            `json:"local,omitempty"`
	Hosted     bool            `json:"hosted,omitempty"`
	Source     string          `json:"source"`
	Ref        string          `json:"ref"`
	Revision   string          `json:"revision"`
	Subpath    string          `json:"subpath"`
	Candidates []CandidateInfo `json:"candidates"`
	Skipped    []SkippedSkill  `json:"skipped"`
	Warnings   []string        `json:"warnings"`
}

type Update struct {
	SkillID   string `json:"skillId"`
	Revision  string `json:"revision"`
	Summary   string `json:"summary"`
	Available bool   `json:"available"`
	Removed   bool   `json:"removed"`
	Error     string `json:"error"`
}
