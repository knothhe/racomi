package desktop

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"racomi/internal/backup"
	"racomi/internal/buildinfo"
	skillstore "racomi/internal/skills"
	"racomi/internal/sources"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

type AppState struct {
	DesktopPreferences     DesktopPreferences        `json:"desktopPreferences"`
	DataDir                string                    `json:"dataDir"`
	HomeDir                string                    `json:"homeDir"`
	State                  skillstore.State          `json:"state"`
	Snapshots              []skillstore.SnapshotInfo `json:"snapshots"`
	DirectoryOptions       []DirectoryOption         `json:"directoryOptions"`
	Projects               []skillstore.Target       `json:"projects"`
	ProjectDirectoryErrors map[string]string         `json:"projectDirectoryErrors"`
	GlobalApplications     []GlobalApplication       `json:"globalApplications"`
	CloudSync              CloudSyncStatus           `json:"cloudSync"`
}

type Service struct {
	initializationError error
	lifecycle           lifecycleState
	desktop             *desktopRuntime
	openWindow          func()
	store               *skillstore.Store
	sources             *sources.Client
	mu                  sync.Mutex
	themeMu             sync.Mutex
	theme               string
	locale              string
	app                 *application.App
	legacyPreview       *skillsManagerSession
	cloud               *cloudSyncRuntime
	r2                  *r2OAuthRuntime
	icon                []byte
}

// NewApplicationService keeps About, updates and error recovery reachable when
// the library cannot open. It never resets or overwrites the failed data.
func NewApplicationService(root string) *Service {
	service, err := New(root)
	if err == nil {
		return service
	}
	return &Service{initializationError: err, desktop: &desktopRuntime{}, theme: "system", locale: "en"}
}

func New(root string) (*Service, error) {
	if root == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return nil, err
		}
		root = filepath.Join(base, buildinfo.DataDirectoryName())
	}
	root, err := expandPath(root)
	if err != nil {
		return nil, err
	}
	store, err := skillstore.Open(root)
	if err != nil {
		return nil, err
	}
	desktop, err := loadDesktopPreferences(store.Root())
	if err != nil {
		return nil, err
	}
	// Sync profiles share the Cloudflare runtime's credential store, including
	// its access-denied state.
	r2 := newR2OAuthRuntime(store.Root())
	cloud, err := loadCloudSync(store.Root(), r2.secrets)
	if err != nil {
		// Optional cloud configuration must never prevent opening the local library.
		cloud = newCloudSyncRuntime(store.Root(), r2.secrets)
		cloud.lastError = err.Error()
	}
	return &Service{desktop: desktop, store: store, sources: sources.New(filepath.Join(store.Root(), "cache", "sources")), theme: "system", locale: "zh-CN", cloud: cloud, r2: r2}, nil
}

// UseIcon gives native surfaces without their own artwork, such as the Linux
// lightweight tray, the application icon. It is not bound to the frontend.
func UseIcon(service *Service, icon []byte) { service.icon = icon }

func Attach(service *Service, app *application.App) {
	service.themeMu.Lock()
	service.app = app
	service.themeMu.Unlock()
	if app != nil {
		if service.r2 != nil {
			service.r2.openURL = app.Browser.OpenURL
		}
		app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(_ *application.ApplicationEvent) {
			service.lifecycle.started.Store(true)
			service.configureMenu()
			service.syncTray()
			application.InvokeAsync(func() { service.startUpdater() })
		})
		app.Event.OnApplicationEvent(events.Common.ThemeChanged, func(_ *application.ApplicationEvent) {
			service.themeMu.Lock()
			defer service.themeMu.Unlock()
			if service.theme == "system" {
				service.applyThemeLocked()
			}
		})
	}
}

// ServiceStartup runs after the platform environment is ready and before windows
// are opened, so the first native background follows the system appearance.
func (s *Service) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	s.startCloudSync(ctx)
	if s.app != nil {
		application.InvokeAsync(func() { s.startUpdater() })
	}
	s.configureMenu()
	s.themeMu.Lock()
	defer s.themeMu.Unlock()
	s.applyThemeLocked()
	return nil
}

// Stop the temporary OAuth listener when the desktop application exits. A
// window close alone does not revoke an existing Cloudflare connection.
func (s *Service) ServiceShutdown() error {
	s.CancelR2OAuth()
	if s.r2 != nil {
		s.r2.workers.Wait()
	}
	return nil
}

// SetTheme colours the native window behind the frontend's matching theme. Wails
// beta.24 exposes dynamic background colours, while native controls keep the
// system appearance. The transparent titlebar shares the frontend background.
func (s *Service) SetTheme(theme string) error {
	if theme != "system" && theme != "light" && theme != "dark" {
		return errors.New("主题必须为 system、light 或 dark")
	}
	s.themeMu.Lock()
	defer s.themeMu.Unlock()
	s.theme = theme
	s.applyThemeLocked()
	return nil
}

func (s *Service) applyThemeLocked() {
	if s.app == nil {
		return
	}
	dark := s.theme == "dark" || (s.theme == "system" && s.app.Env.IsDarkMode())
	colour := application.NewRGB(244, 244, 246)
	if dark {
		colour = application.NewRGB(26, 26, 30)
	}
	for _, window := range s.app.Window.GetAll() {
		window.SetBackgroundColour(colour)
	}
}

// ReadSkillDocument reads only the bounded document from an indexed local object.
// Import and deployment verify the whole object; reading its documentation must
// not wait for source network requests or rehash unrelated assets.
func (s *Service) ReadSkillDocument(skillID string) (string, error) {
	// A concurrent install/removal can retire the indexed object. Keep it alive
	// until this read finishes without taking the service's mutation/network lock.
	release := s.store.SuspendContentCleanup()
	defer release()
	state, err := s.store.State()
	if err != nil {
		return "", err
	}
	var contentHash string
	for _, skill := range state.Skills {
		if skill.ID == skillID {
			contentHash = skill.ContentHash
			break
		}
	}
	if contentHash == "" {
		return "", errors.New("技能不存在")
	}
	objects, err := os.OpenRoot(filepath.Join(s.store.Root(), "objects"))
	if err != nil {
		return "", err
	}
	defer objects.Close()
	objectInfo, err := objects.Lstat(contentHash)
	if err != nil {
		return "", err
	}
	if !objectInfo.IsDir() || objectInfo.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("技能内容必须为本地普通目录")
	}
	root, err := objects.OpenRoot(contentHash)
	if err != nil {
		return "", err
	}
	defer root.Close()
	currentObject, err := root.Stat(".")
	if err != nil {
		return "", err
	}
	if !os.SameFile(objectInfo, currentObject) {
		return "", errors.New("读取时技能目录发生变化")
	}
	info, err := root.Lstat("SKILL.md")
	if err != nil {
		return "", err
	}
	const maxDocumentSize = 1 << 20
	if !info.Mode().IsRegular() || info.Size() > maxDocumentSize {
		return "", errors.New("SKILL.md 必须为不超过 1 MiB 的普通文件")
	}
	file, err := root.Open("SKILL.md")
	if err != nil {
		return "", err
	}
	defer file.Close()
	current, err := file.Stat()
	if err != nil {
		return "", err
	}
	sameVersion := func(before, after os.FileInfo) bool {
		return after.Mode().IsRegular() && os.SameFile(before, after) && before.Size() == after.Size() && before.ModTime().Equal(after.ModTime())
	}
	if !sameVersion(info, current) {
		return "", errors.New("读取时技能文档发生变化")
	}
	content, err := io.ReadAll(io.LimitReader(file, maxDocumentSize+1))
	if err != nil {
		return "", err
	}
	if len(content) > maxDocumentSize {
		return "", errors.New("SKILL.md 不能超过 1 MiB")
	}
	// Check both the open file and its name after the read, catching concurrent
	// edits and replacements without walking the rest of the skill directory.
	after, err := file.Stat()
	if err != nil {
		return "", err
	}
	named, err := root.Lstat("SKILL.md")
	if err != nil {
		return "", err
	}
	if !sameVersion(current, after) || !sameVersion(current, named) || int64(len(content)) != current.Size() {
		return "", errors.New("读取时技能文档发生变化")
	}
	if !utf8.Valid(content) {
		return "", errors.New("SKILL.md 必须为有效的 UTF-8 文本")
	}
	return string(content), nil
}

func (s *Service) GetState() (AppState, error) {
	if s.initializationError != nil {
		return AppState{}, s.initializationError
	}
	// Sample completion before the library. A concurrent sync may make this
	// marker older than the returned data, which polling can safely reload;
	// sampling it afterward could acknowledge data this response never read.
	cloud := s.GetCloudSyncStatus()
	state, err := s.store.State()
	if err != nil {
		return AppState{}, err
	}
	snapshots, err := s.store.Snapshots()
	if err != nil {
		return AppState{}, err
	}
	directories, err := codexDirectoryOptions()
	if err != nil {
		return AppState{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return AppState{}, err
	}
	home, err = filepath.Abs(home)
	if err != nil {
		return AppState{}, err
	}
	globals, err := globalApplications(state)
	if err != nil {
		return AppState{}, err
	}
	projects := []skillstore.Target{}
	projectDirectoryErrors := map[string]string{}
	for _, target := range state.Targets {
		if target.Kind == "project" {
			projects = append(projects, target)
			if err := skillstore.ValidateProjectRoot(target.ProjectRoot); err != nil {
				projectDirectoryErrors[target.ID] = err.Error()
			}
		}
	}
	return AppState{DesktopPreferences: s.desktopPreferences(), DataDir: s.store.Root(), HomeDir: applicationPath(home), State: state, Snapshots: snapshots, DirectoryOptions: directories, Projects: projects, ProjectDirectoryErrors: projectDirectoryErrors, GlobalApplications: globals, CloudSync: cloud}, nil
}

func (s *Service) Search(ctx context.Context, query string) ([]sources.SearchResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return s.sources.Search(ctx, query)
}

func (s *Service) ListSkills(ctx context.Context, page int) (sources.SkillListPage, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return s.sources.ListSkills(ctx, page)
}

func (s *Service) CheckHubAvailability(ctx context.Context, inputs []string) ([]sources.HubAvailability, error) {
	return s.sources.CheckHubAvailability(ctx, inputs)
}

func (s *Service) Preview(ctx context.Context, source, ref, subpath string) (sources.Preview, error) {
	ctx, cancel := context.WithTimeout(ctx, sources.SourceOperationTimeout)
	defer cancel()
	return s.sources.Preview(ctx, source, ref, subpath)
}

func (s *Service) PreviewHubRepository(ctx context.Context, source, ref, subpath string) (sources.Preview, error) {
	ctx, cancel := context.WithTimeout(ctx, sources.SourceOperationTimeout)
	defer cancel()
	return s.sources.PreviewHubRepository(ctx, source, ref, subpath)
}

func (s *Service) PreviewLocal(ctx context.Context, path string) (sources.Preview, error) {
	path, err := expandPath(path)
	if err != nil {
		return sources.Preview{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	return s.sources.PreviewLocal(ctx, path)
}

func (s *Service) Install(previewID string, paths []string) (AppState, error) {
	return s.mutate(func() error { return s.installFromPreview(previewID, paths) })
}

func (s *Service) CheckUpdates(ctx context.Context) ([]sources.Update, error) {
	return s.checkSourceUpdates(ctx)
}

func (s *Service) UpdateSkill(ctx context.Context, id string) (AppState, error) {
	return s.UpdateSkills(ctx, []string{id})
}

func (s *Service) RemoveSkill(id string) (AppState, error) {
	return s.RemoveSkills([]string{id})
}
func (s *Service) SaveSettings(settings skillstore.Settings) (AppState, error) {
	return s.mutate(func() error { return s.store.SaveSettings(settings) })
}
func (s *Service) SavePreset(preset skillstore.Preset) (AppState, error) {
	return s.mutate(func() error { _, err := s.store.SavePreset(preset); return err })
}
func (s *Service) DeletePreset(id string) (AppState, error) {
	return s.mutate(func() error { return s.store.DeletePreset(id) })
}
func (s *Service) SaveTarget(target skillstore.Target) (AppState, error) {
	return s.mutate(func() error { _, err := s.store.SaveTarget(target); return err })
}
func (s *Service) DeleteTarget(id string) (AppState, error) {
	return s.mutate(func() error { return s.store.DeleteTarget(id) })
}
func (s *Service) SetTargetPresets(id string, presetIDs []string) (AppState, error) {
	return s.mutate(func() error { return s.store.SetTargetPresets(id, presetIDs) })
}
func (s *Service) SetTargetSelection(id string, skillIDs, presetIDs []string) (AppState, error) {
	return s.mutate(func() error { return s.store.SetTargetSelection(id, skillIDs, presetIDs) })
}
func (s *Service) PlanTarget(id string) (skillstore.ApplyPlan, error) { return s.store.Plan(id) }
func (s *Service) ApplyTarget(id string) (AppState, error) {
	return s.mutate(func() error { return s.store.Apply(id) })
}
func (s *Service) CreateSnapshot() (AppState, error) {
	return s.mutate(func() error { _, err := s.store.Snapshot("手动快照"); return err })
}
func (s *Service) RestoreSnapshot(id string) (AppState, error) {
	return s.mutate(func() error { return s.store.RestoreSnapshot(id) })
}
func (s *Service) ExportBackup(filename string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	filename, err := expandPath(filename)
	if err != nil {
		return "", err
	}
	state, err := s.store.State()
	if err != nil {
		return "", err
	}
	if err = backup.Export(s.store.Root(), state, filename); err != nil {
		return "", err
	}
	return filename, nil
}
func (s *Service) ImportBackup(filename string) (AppState, error) {
	return s.mutate(func() error {
		filename, err := expandPath(filename)
		if err != nil {
			return err
		}
		return backup.Import(s.store, filename)
	})
}

func (s *Service) ChooseDirectory() (string, error) {
	if s.app == nil {
		return "", errors.New(s.nativeText("Directory selection is available only in the desktop app", "目录选择仅在桌面应用中可用", "目錄選擇僅在桌面應用程式中可用"))
	}
	return s.openFileDialog().SetTitle(s.nativeText("Choose target directory", "选择目标目录", "選擇目標目錄")).CanChooseDirectories(true).CanChooseFiles(false).ShowHiddenFiles(true).PromptForSingleSelection()
}
func (s *Service) ChooseExportFile() (string, error) {
	if s.app == nil {
		return "", errors.New(s.nativeText("File selection is available only in the desktop app", "文件选择仅在桌面应用中可用", "檔案選擇僅在桌面應用程式中可用"))
	}
	return s.saveFileDialog().SetFilename("racomi-"+time.Now().Format("2006-01-02")+".zip").AddFilter(s.nativeText("racomi backup", "racomi 备份", "racomi 備份"), "*.zip").PromptForSingleSelection()
}
func (s *Service) ChooseImportFile() (string, error) {
	if s.app == nil {
		return "", errors.New(s.nativeText("File selection is available only in the desktop app", "文件选择仅在桌面应用中可用", "檔案選擇僅在桌面應用程式中可用"))
	}
	return s.openFileDialog().SetTitle(s.nativeText("Import racomi backup", "导入 racomi 备份", "匯入 racomi 備份")).AddFilter(s.nativeText("racomi backup", "racomi 备份", "racomi 備份"), "*.zip").PromptForSingleSelection()
}

func (s *Service) mutate(change func() error) (AppState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := change(); err != nil {
		return AppState{}, err
	}
	return s.GetState()
}

func expandPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("路径不能为空")
	}
	if value == "~" || strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if value == "~" {
			value = home
		} else {
			value = filepath.Join(home, strings.TrimPrefix(value, "~/"))
		}
	}
	if !filepath.IsAbs(value) {
		return "", errors.New("请输入绝对路径")
	}
	return filepath.Clean(value), nil
}
