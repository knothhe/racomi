package desktop

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"time"

	skillstore "racomi/internal/skills"
	"racomi/internal/skillsmanager"
)

type SkillsManagerPreview struct {
	ID           string   `json:"id"`
	DatabasePath string   `json:"databasePath"`
	SkillCount   int      `json:"skillCount"`
	PresetCount  int      `json:"presetCount"`
	SourceCount  int      `json:"sourceCount"`
	Warnings     []string `json:"warnings"`
	SkillNames   []string `json:"skillNames"`
	PresetNames  []string `json:"presetNames"`
}

type skillsManagerSession struct {
	id      string
	expires time.Time
	bundle  *skillsmanager.Bundle
}

type SkillsManagerImport struct {
	App    AppState                `json:"app"`
	Result skillstore.ImportResult `json:"result"`
}

func (s *Service) DefaultSkillsManagerPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".skills-manager", "skills-manager.db"), nil
}

func (s *Service) ChooseSkillsManagerFile() (string, error) {
	if s.app == nil {
		return "", errors.New(s.nativeText("File selection is available only in the desktop app", "文件选择仅在桌面应用中可用", "檔案選擇僅在桌面應用程式中可用"))
	}
	return s.app.Dialog.OpenFile().SetTitle(s.nativeText("Choose skills-manager database", "选择 skills-manager 数据库", "選擇 skills-manager 資料庫")).ShowHiddenFiles(true).AddFilter(s.nativeText("SQLite database", "SQLite 数据库", "SQLite 資料庫"), "*.db;*.sqlite;*.sqlite3").PromptForSingleSelection()
}

func (s *Service) PreviewSkillsManager(filename string) (SkillsManagerPreview, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// A failed new preview must not leave an older import token usable.
	s.legacyPreview = nil
	filename, err := expandPath(filename)
	if err != nil {
		return SkillsManagerPreview{}, err
	}
	state, err := s.store.State()
	if err != nil {
		return SkillsManagerPreview{}, err
	}
	bundle, err := skillsmanager.Read(filename, state)
	if err != nil {
		return SkillsManagerPreview{}, err
	}
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		return SkillsManagerPreview{}, err
	}
	id := hex.EncodeToString(random[:])
	s.legacyPreview = &skillsManagerSession{id: id, expires: time.Now().Add(15 * time.Minute), bundle: bundle}
	preview := SkillsManagerPreview{ID: id, DatabasePath: bundle.DatabasePath, SkillCount: len(bundle.Candidates), PresetCount: len(bundle.Presets), SourceCount: len(bundle.Sources), Warnings: bundle.Warnings, SkillNames: []string{}, PresetNames: []string{}}
	for _, candidate := range bundle.Candidates {
		preview.SkillNames = append(preview.SkillNames, candidate.Skill.Name)
	}
	for _, preset := range bundle.Presets {
		preview.PresetNames = append(preview.PresetNames, preset.Name)
	}
	return preview, nil
}

func (s *Service) ImportSkillsManager(previewID string) (SkillsManagerImport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.legacyPreview
	if session == nil || previewID == "" || session.id != previewID || time.Now().After(session.expires) {
		return SkillsManagerImport{}, errors.New("导入预览不存在或已过期，请重新预览数据库")
	}
	result, err := s.store.MergeImport(session.bundle.Candidates, session.bundle.Presets, session.bundle.Sources)
	if err != nil {
		return SkillsManagerImport{}, err
	}
	s.legacyPreview = nil
	app, err := s.GetState()
	if err != nil {
		return SkillsManagerImport{}, err
	}
	return SkillsManagerImport{App: app, Result: result}, nil
}
