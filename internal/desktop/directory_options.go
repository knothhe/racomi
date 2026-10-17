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
			ID: "codex-user", Name: "Agent Skills", Kind: "global",
			Path:        filepath.Join(home, ".agents", "skills"),
			Description: "Agent Skills 共享技能目录，供兼容此路径的 Agent 读取，适用于所有项目。",
		},
		{
			ID: "codex-legacy", Name: "Codex 兼容目录", Kind: "global",
			Path:        filepath.Join(codexHome, "skills"),
			Description: "保留已有 Codex 技能配置，遵循 CODEX_HOME；新配置建议使用 ~/.agents/skills。",
		},
		{
			ID: "codex-project", Name: "Agent Skills", Kind: "project", Path: "",
			Description: "Agent Skills 共享技能目录，供兼容此路径的 Agent 读取，适用于当前项目。",
		},
	}, nil
}
