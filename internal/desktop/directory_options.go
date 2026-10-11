package desktop

import (
	"os"
	"path/filepath"
	"strings"
)

// DirectoryOption describes a target shortcut, not a saved or deployed target.
// Project shortcuts leave Path empty: the caller must choose a repository root
// and pass it to SaveTarget, which appends .agents/skills.
type DirectoryOption struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	Kind        string `json:"kind"`
	Description string `json:"description"`
}

func codexDirectoryOptions() ([]DirectoryOption, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	home, err = filepath.Abs(home)
	if err != nil {
		return nil, err
	}
	codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME"))
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	} else if codexHome == "~" || strings.HasPrefix(codexHome, "~/") {
		codexHome = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(codexHome, "~"), "/"))
	}
	codexHome, err = filepath.Abs(codexHome)
	if err != nil {
		return nil, err
	}
	return []DirectoryOption{
		{
			ID: "codex-user", Name: "Codex 用户目录", Kind: "global",
			Path:        filepath.Join(home, ".agents", "skills"),
			Description: "当前推荐的用户技能目录，适用于所有项目。",
		},
		{
			ID: "codex-legacy", Name: "Codex 兼容目录", Kind: "global",
			Path:        filepath.Join(codexHome, "skills"),
			Description: "兼容已有 Codex skills，遵循 CODEX_HOME，默认 ~/.codex/skills。",
		},
		{
			ID: "codex-project", Name: "Codex 项目目录", Kind: "project", Path: "",
			Description: "选择项目根目录，将使用该项目下的 .agents/skills。",
		},
	}, nil
}
