package desktop

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type windowsUpdatePlan struct {
	PID        int        `json:"pid"`
	Executable string     `json:"executable"`
	Staged     string     `json:"staged"`
	SHA256     string     `json:"sha256"`
	Feed       updateFeed `json:"feed"`
}

func updateDirectoryWritable(directory string) bool {
	file, err := os.CreateTemp(directory, ".racomi-write-*")
	if err != nil {
		return false
	}
	file.Close()
	return os.Remove(file.Name()) == nil
}

func executableDigest(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func (u *windowsUpdater) install(staged string) {
	s := u.service
	go s.requestLifecycle("update", func() {
		// The helper is a private copy of this trusted executable. It waits for
		// this process to exit, since Windows locks loaded executable files.
		if err := u.startHelper(staged); err != nil {
			s.lifecycle.mu.Lock()
			s.lifecycle.running = false
			s.lifecycle.mu.Unlock()
			s.lifecycle.permit.Store(false)
			s.lifecycle.stopping.Store(false)
			s.app.Event.Emit("racomi:lifecycle-resumed")
			u.report(true, err)
			return
		}
		s.app.Quit()
	})
}

func (u *windowsUpdater) startHelper(staged string) error {
	data, err := os.ReadFile(u.executable)
	if err != nil {
		return err
	}
	helper, err := os.CreateTemp(filepath.Dir(u.executable), updatePrefix+"helper-*.exe")
	if err != nil {
		return err
	}
	name := helper.Name()
	ok := false
	defer func() {
		if !ok {
			os.Remove(name)
			os.Remove(name + ".json")
		}
	}()
	_, err = helper.Write(data)
	closeErr := helper.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	digest, err := executableDigest(staged)
	if err != nil {
		return err
	}
	u.mu.Lock()
	feed := u.stagedFeed
	u.mu.Unlock()
	plan, err := json.Marshal(windowsUpdatePlan{os.Getpid(), u.executable, staged, digest, feed})
	if err != nil {
		return err
	}
	if err = os.WriteFile(name+".json", plan, 0o600); err != nil {
		return err
	}
	command := exec.Command(name, "--racomi-apply-update", name+".json")
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err = command.Start(); err != nil {
		return err
	}
	_ = command.Process.Release()
	ok = true
	return nil
}

func validateWindowsUpdatePlan(plan windowsUpdatePlan, helper, planPath string) error {
	directory := filepath.Dir(helper)
	if plan.PID <= 1 || !filepath.IsAbs(helper) || planPath != helper+".json" || !strings.HasPrefix(filepath.Base(helper), updatePrefix+"helper-") {
		return errors.New("invalid Windows update helper identity")
	}
	for _, path := range []string{plan.Executable, plan.Staged, helper} {
		if !filepath.IsAbs(path) || !strings.EqualFold(filepath.Dir(path), directory) {
			return errors.New("update paths must share the installation directory")
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("update path is not a regular file")
		}
	}
	if !strings.HasPrefix(filepath.Base(plan.Staged), updatePrefix) || !strings.HasSuffix(plan.Staged, ".exe") || strings.EqualFold(plan.Staged, helper) || strings.EqualFold(plan.Executable, helper) || strings.EqualFold(plan.Executable, plan.Staged) {
		return errors.New("invalid Windows update staging paths")
	}
	digest, err := executableDigest(plan.Staged)
	if err != nil {
		return err
	}
	if digest != plan.SHA256 {
		return errors.New("The downloaded update does not match the update feed.")
	}
	return verifyStagedIdentity(plan.Staged, plan.Feed)
}

func waitForWindowsProcess(pid int, timeout time.Duration) error {
	process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return nil
	} // Already exited.
	if err != nil {
		return err
	}
	defer windows.CloseHandle(process)
	status, err := windows.WaitForSingleObject(process, uint32(timeout/time.Millisecond))
	if err != nil {
		return err
	}
	if status != windows.WAIT_OBJECT_0 {
		return errors.New("racomi did not exit; the installed application was preserved")
	}
	return nil
}

var replaceFile = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReplaceFileW")

// ReplaceFile preserves the old file as a backup in the same atomic operation.
// Retrying tolerates a scanner briefly holding the just-terminated executable.
func replaceWindowsExecutable(executable, staged, backup string) error {
	target, err := windows.UTF16PtrFromString(executable)
	if err != nil {
		return err
	}
	source, err := windows.UTF16PtrFromString(staged)
	if err != nil {
		return err
	}
	previous, err := windows.UTF16PtrFromString(backup)
	if err != nil {
		return err
	}
	result, _, callErr := replaceFile.Call(uintptr(unsafe.Pointer(target)), uintptr(unsafe.Pointer(source)), uintptr(unsafe.Pointer(previous)), 0, 0, 0)
	if result != 0 {
		return nil
	}
	// Some ReplaceFile failures can leave the original under its backup name.
	if _, err := os.Stat(executable); os.IsNotExist(err) {
		if restoreErr := os.Rename(backup, executable); restoreErr != nil {
			return fmt.Errorf("replacement failed: %v; restore failed: %w", callErr, restoreErr)
		}
	}
	return callErr
}

func applyWindowsUpdate(plan windowsUpdatePlan) error {
	if err := waitForWindowsProcess(plan.PID, time.Minute); err != nil {
		return err
	}
	// Recheck after waiting; nothing is installed if staging changed meanwhile.
	digest, err := executableDigest(plan.Staged)
	if err != nil || digest != plan.SHA256 {
		return errors.New("The downloaded update does not match the update feed.")
	}
	backup := plan.Staged + ".previous"
	for deadline := time.Now().Add(10 * time.Second); ; {
		err = replaceWindowsExecutable(plan.Executable, plan.Staged, backup)
		if err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err != nil {
		return err
	}
	command := exec.Command(plan.Executable)
	// Suppress console allocation without STARTF_USESHOWWINDOW/SW_HIDE, which
	// would also hide the GUI application's first window after the restart.
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	if err = command.Start(); err != nil {
		if restoreErr := replaceWindowsExecutable(plan.Executable, backup, plan.Staged); restoreErr != nil {
			return fmt.Errorf("restart failed: %v; rollback failed: %w", err, restoreErr)
		}
		previous := exec.Command(plan.Executable)
		previous.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
		if restartErr := previous.Start(); restartErr == nil {
			_ = previous.Process.Release()
		}
		return fmt.Errorf("restart failed; previous executable restored: %w", err)
	}
	_ = command.Process.Release()
	_ = os.Remove(backup)
	return nil
}

// RunUpdateHelper is handled before Wails and the single-instance lock. The
// helper has no UI and never opens the skill library or modifies user data.
func RunUpdateHelper() bool {
	if len(os.Args) < 2 || os.Args[1] != "--racomi-apply-update" {
		return false
	}
	if len(os.Args) != 3 {
		return true
	}
	helper, err := os.Executable()
	if err != nil {
		return true
	}
	if !strings.HasPrefix(filepath.Base(helper), updatePrefix+"helper-") || os.Args[2] != helper+".json" {
		return true
	}
	data, err := os.ReadFile(os.Args[2])
	var plan windowsUpdatePlan
	if err == nil {
		err = json.Unmarshal(data, &plan)
	}
	if err == nil {
		err = validateWindowsUpdatePlan(plan, helper, os.Args[2])
	}
	if err == nil {
		err = applyWindowsUpdate(plan)
	}
	if err != nil {
		log.Printf("Windows update failed: %v", err)
		_ = os.WriteFile(helper+".log", []byte(err.Error()+"\n"), 0o600)
	}
	_ = os.Remove(helper + ".json")
	return true
}

func RelaunchAfterUpdate()    {}
func WaitForUpdatedInstance() {}
