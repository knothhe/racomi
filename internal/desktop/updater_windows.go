package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"racomi/internal/buildinfo"
	"runtime"

	"sync"
	"syscall"

	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// The Windows updater downloads the
// signed release archive, checks the extracted build identity, then asks the
// page to save before an independent helper replaces the EXE after process exit.
type windowsUpdater struct {
	mu         sync.Mutex
	service    *Service
	executable string
	reason     string
	automatic  bool
	busy       bool
	staged     string // A verified executable waiting for the page to save.
	stagedFeed linuxUpdateFeed
	started    bool
}

var updater windowsUpdater

const (
	updateFirstCheck = time.Minute
	updateInterval   = 24 * time.Hour
	updatePrefix     = ".racomi-update-"
)

// updateHTTPClient is replaced in tests; release downloads follow redirects.
var updateHTTPClient = http.DefaultClient

func updaterPreferencesPath(s *Service) string {
	if s.store == nil {
		return ""
	}
	return filepath.Join(s.store.Root(), "updater.json")
}

func loadAutomaticUpdates(path string) bool {
	if path == "" {
		return true
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return true
	}
	var saved struct {
		Automatic *bool `json:"automatic"`
	}
	if json.Unmarshal(data, &saved) != nil || saved.Automatic == nil {
		return true
	}
	return *saved.Automatic
}

// updatableExecutable rejects locations an in-place swap cannot or should not
// replace: temporary go run builds and directories the user cannot write.
func updatableExecutable() (string, string) {
	exe, err := os.Executable()
	if err != nil {
		return "", "The desktop updater is unavailable."
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if !autostartInstalled(exe) || !updateDirectoryWritable(filepath.Dir(exe)) {
		return "", "Move racomi to a folder you can write before enabling updates."
	}
	return exe, ""
}

func nativeStartUpdater(s *Service) {
	u := &updater
	u.mu.Lock()
	if u.started {
		u.mu.Unlock()
		return
	}
	u.started, u.service = true, s
	u.executable, u.reason = updatableExecutable()
	u.automatic = loadAutomaticUpdates(updaterPreferencesPath(s))
	exe, reason := u.executable, u.reason
	u.mu.Unlock()
	if reason != "" {
		return
	}
	// A single instance owns this directory; leftovers come from an earlier
	// update that was cancelled or interrupted.
	if leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), updatePrefix+"*.exe")); len(leftovers) > 0 {
		for _, path := range leftovers {
			_ = os.Remove(path)
		}
	}
	go func() {
		time.Sleep(updateFirstCheck)
		for {
			u.mu.Lock()
			automatic := u.automatic
			u.mu.Unlock()
			if automatic {
				u.check(false)
			}
			time.Sleep(updateInterval)
		}
	}()
}

func nativeUpdaterState() UpdaterState {
	u := &updater
	u.mu.Lock()
	defer u.mu.Unlock()
	if !u.started {
		return UpdaterState{Reason: "The desktop updater is unavailable."}
	}
	if u.reason != "" {
		return UpdaterState{Reason: u.reason}
	}
	return UpdaterState{Available: true, Automatic: u.automatic, CanCheck: !u.busy}
}

func nativeCheckUpdates() { go updater.check(true) }

func nativeSetAutomaticUpdates(on bool) {
	u := &updater
	u.mu.Lock()
	defer u.mu.Unlock()
	u.automatic = on
	if path := updaterPreferencesPath(u.service); path != "" {
		data, _ := json.Marshal(struct {
			Automatic bool `json:"automatic"`
		}{on})
		if err := os.WriteFile(path+".tmp", data, 0o600); err == nil {
			_ = os.Rename(path+".tmp", path)
		}
	}
}

func (u *windowsUpdater) check(manual bool) {
	u.mu.Lock()
	if u.busy || u.reason != "" || u.service == nil {
		u.mu.Unlock()
		return
	}
	s, staged := u.service, u.staged
	if staged != "" {
		u.mu.Unlock()
		// A verified update is already waiting; only the user resumes its
		// save-and-restart step, never a background check.
		if manual {
			u.install(staged)
		}
		return
	}
	u.busy = true
	u.mu.Unlock()
	done := func() { u.mu.Lock(); u.busy = false; u.mu.Unlock() }
	feed, asset, err := fetchWindowsFeed()
	if err != nil {
		done()
		u.report(manual, err)
		return
	}
	current := buildinfo.Current().Version
	if compareVersions(feed.Version, current) <= 0 {
		done()
		if manual {
			s.showMessage(s.app.Dialog.Info(), s.nativeText("racomi is up to date", "racomi 已是最新版本", "racomi 已是最新版本"),
				s.nativeText(fmt.Sprintf("Version %s is the newest available version.", current), fmt.Sprintf("%s 是当前可用的最新版本。", current), fmt.Sprintf("%s 是目前可用的最新版本。", current)), true)
		}
		return
	}
	dialog := s.app.Dialog.Question()
	dialog.SetTitle(s.nativeText("A new version of racomi is available", "racomi 有新版本", "racomi 有新版本"))
	dialog.SetMessage(s.nativeText(
		fmt.Sprintf("racomi %s is available; you have %s. Install it now? racomi saves your changes and restarts.", feed.Version, current),
		fmt.Sprintf("racomi %s 已发布，当前为 %s。现在安装吗？racomi 会先保存更改再重启。", feed.Version, current),
		fmt.Sprintf("racomi %s 已發布，目前為 %s。現在安裝嗎？racomi 會先儲存變更再重新啟動。", feed.Version, current)))
	install := dialog.AddButton(s.nativeText("Install and Restart", "安装并重启", "安裝並重新啟動"))
	dialog.AddButton(s.nativeText("Later", "稍后", "稍後")).SetAsCancel()
	// Closing the dialog runs no button callback, so the check is finished
	// before it opens and the download claims the busy state for itself.
	done()
	install.SetAsDefault().OnClick(func() {
		u.mu.Lock()
		if u.busy {
			u.mu.Unlock()
			return
		}
		u.busy = true
		u.mu.Unlock()
		path, err := u.download(feed, asset)
		done()
		if err != nil {
			u.report(true, err)
			return
		}
		u.mu.Lock()
		u.staged = path
		u.stagedFeed = feed
		u.mu.Unlock()
		u.install(path)
	})
	if window := u.ownerWindow(manual); window != nil {
		dialog.AttachToWindow(window)
	}
	dialog.Show()
}

// Automatic checks must not reopen a released lightweight window.
func (u *windowsUpdater) ownerWindow(manual bool) application.Window {
	if !manual {
		return nil
	}
	return u.service.dialogWindow()
}

func (u *windowsUpdater) report(manual bool, err error) {
	if !manual {
		log.Printf("update check failed: %v", err)
		return
	}
	s := u.service
	s.showMessage(s.app.Dialog.Error(), s.nativeText("Update failed", "更新失败", "更新失敗"), s.nativeText(err.Error(), translateUpdateError(err.Error(), false), translateUpdateError(err.Error(), true)), true)
}

func (s *Service) showMessage(dialog *application.MessageDialog, title, message string, attach bool) {
	dialog.SetTitle(title)
	dialog.SetMessage(message)
	if attach {
		if window := s.dialogWindow(); window != nil {
			dialog.AttachToWindow(window)
		}
	}
	dialog.Show()
}

func fetchWindowsFeed() (linuxUpdateFeed, linuxUpdateAsset, error) {
	data, err := httpGet(buildinfo.FeedURL, maxUpdateFeedBytes, time.Minute)
	if err != nil {
		return linuxUpdateFeed{}, linuxUpdateAsset{}, err
	}
	return parseWindowsUpdateFeed(data, runtime.GOARCH)
}

func httpGet(url string, limit int64, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "racomi/"+buildinfo.Current().Version)
	response, err := updateHTTPClient.Do(request)
	if err != nil {
		return nil, errors.New("The update server could not be reached.")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("The update server could not be reached.")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, errors.New("The update server could not be reached.")
	}
	if int64(len(data)) > limit {
		return nil, errors.New("The downloaded update does not match the update feed.")
	}
	return data, nil
}

// download stages a verified executable beside the installed one, so the final
// swap is a rename on the same filesystem.
func (u *windowsUpdater) download(feed linuxUpdateFeed, asset linuxUpdateAsset) (string, error) {
	archive, err := httpGet(asset.URL, asset.Length, 10*time.Minute)
	if err != nil {
		return "", err
	}
	if err = verifyLinuxUpdateArchive(archive, asset, buildinfo.PublicKey); err != nil {
		return "", err
	}
	stage, err := os.CreateTemp(filepath.Dir(u.executable), updatePrefix+"*.exe")
	if err != nil {
		return "", err
	}
	keep := false
	defer func() {
		if !keep {
			os.Remove(stage.Name())
		}
	}()
	if err = extractWindowsUpdate(archive, stage); err != nil {
		stage.Close()
		return "", err
	}
	if err = stage.Chmod(0o755); err != nil {
		stage.Close()
		return "", err
	}
	if err = stage.Close(); err != nil {
		return "", err
	}
	if err = verifyStagedIdentity(stage.Name(), feed); err != nil {
		return "", err
	}
	keep = true
	return stage.Name(), nil
}

// The archive signature proves origin; the build identity proves it is the
// release the feed names, for this installation's channel and architecture.
func verifyStagedIdentity(path string, feed linuxUpdateFeed) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, path, "--build-info")
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := command.Output()
	if err != nil {
		return errors.New("The downloaded update could not be started.")
	}
	var info buildinfo.Info
	if err = json.Unmarshal(bytes.TrimSpace(output), &info); err != nil {
		return errors.New("The downloaded update could not be started.")
	}
	current := buildinfo.Current()
	if info.ID != current.ID || info.Channel != "stable" || info.Executor != "ci" || info.Dirty || info.OS != "windows" || info.Arch != runtime.GOARCH || info.Version != feed.Version || info.Commit != feed.Commit {
		return errors.New("The downloaded update does not match its release identity.")
	}
	return nil
}

// Native dialogs are created in Go, so the frontend's backend message table is
// not available; these mirror its translations for the updater's own errors.
func translateUpdateError(message string, traditional bool) string {
	for _, row := range updateErrorTranslations {
		if row[0] == message {
			if traditional {
				return row[2]
			}
			return row[1]
		}
	}
	return message
}

var updateErrorTranslations = [][3]string{
	{"The update server could not be reached.", "无法连接更新服务器。", "無法連線更新伺服器。"},
	{"The update feed is invalid.", "更新源内容无效。", "更新來源內容無效。"},
	{"The update feed is too large.", "更新源内容过大。", "更新來源內容過大。"},
	{"No update is published for this architecture.", "此架构尚无可用更新。", "此架構尚無可用更新。"},
	{"The update feed is not configured.", "尚未配置更新源。", "尚未設定更新來源。"},
	{"The update signature is invalid.", "更新签名无效。", "更新簽章無效。"},
	{"The downloaded update does not match the update feed.", "下载的更新与更新源不符。", "下載的更新與更新來源不符。"},
	{"The update archive is invalid.", "更新包无效。", "更新封包無效。"},
	{"The update archive has no racomi executable.", "更新包中没有 racomi 程序。", "更新封包中沒有 racomi 程式。"},
	{"The downloaded update could not be started.", "下载的更新无法启动。", "下載的更新無法啟動。"},
	{"The downloaded update does not match its release identity.", "下载的更新与发布身份不符。", "下載的更新與發布身分不符。"},
}
