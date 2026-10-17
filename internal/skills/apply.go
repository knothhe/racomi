package skills

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
)

func emptyPlan(targetID string) ApplyPlan {
	return ApplyPlan{TargetID: targetID, Add: []string{}, Update: []string{}, Remove: []string{}, Conflicts: []string{}, BlockingConflicts: []string{}, Overwrite: []string{}, Directories: []DirectoryPlan{}}
}

func (l *Store) Plan(targetID string) (ApplyPlan, error) { return l.PlanTargets([]string{targetID}) }

func (l *Store) PlanTargets(targetIDs []string) (ApplyPlan, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	state, err := l.readState()
	if err != nil {
		return ApplyPlan{}, err
	}
	return l.planTargets(state, targetIDs)
}

func (l *Store) planTargets(state State, targetIDs []string) (ApplyPlan, error) {
	plan := emptyPlan("")
	if len(targetIDs) == 1 {
		plan.TargetID = targetIDs[0]
	}
	for _, id := range unique(targetIDs) {
		index := slices.IndexFunc(state.Targets, func(t Target) bool { return t.ID == id })
		if index < 0 {
			return plan, errors.New("target not found")
		}
		next, err := l.plan(state, state.Targets[index])
		if err != nil {
			return plan, err
		}
		plan.Directories = append(plan.Directories, next.Directories...)
	}
	aggregatePlan(&plan)
	return plan, nil
}

func aggregatePlan(plan *ApplyPlan) {
	for _, directory := range plan.Directories {
		plan.Add = append(plan.Add, directory.Add...)
		plan.Update = append(plan.Update, directory.Update...)
		plan.Remove = append(plan.Remove, directory.Remove...)
		plan.Overwrite = append(plan.Overwrite, directory.Overwrite...)
		for _, conflict := range directory.Conflicts {
			if len(plan.Directories) == 1 {
				plan.Conflicts = append(plan.Conflicts, conflict)
			} else {
				plan.Conflicts = append(plan.Conflicts, directory.Path+": "+conflict)
			}
		}
		for _, conflict := range directory.BlockingConflicts {
			if len(plan.Directories) == 1 {
				plan.BlockingConflicts = append(plan.BlockingConflicts, conflict)
			} else {
				plan.BlockingConflicts = append(plan.BlockingConflicts, directory.Path+": "+conflict)
			}
		}
	}
	plan.Add, plan.Update, plan.Remove, plan.Conflicts = unique(plan.Add), unique(plan.Update), unique(plan.Remove), unique(plan.Conflicts)
	plan.BlockingConflicts, plan.Overwrite = unique(plan.BlockingConflicts), unique(plan.Overwrite)
	sort.Strings(plan.Add)
	sort.Strings(plan.Update)
	sort.Strings(plan.Remove)
	sort.Strings(plan.Conflicts)
	sort.Strings(plan.BlockingConflicts)
	sort.Strings(plan.Overwrite)
}

func (l *Store) plan(state State, target Target) (ApplyPlan, error) {
	plan := emptyPlan(target.ID)
	if target.Kind == "project" {
		if err := ValidateProjectRoot(target.ProjectRoot); err != nil {
			return plan, err
		}
	}
	for _, path := range allTargetPaths(target) {
		directory, err := l.planDirectory(state, target, path)
		if err != nil {
			return plan, err
		}
		plan.Directories = append(plan.Directories, directory)
	}
	aggregatePlan(&plan)
	return plan, nil
}

// A symlink deployment is owned only when the link still points to its exact
// immutable object. Never follow a replacement link into arbitrary user data.
func (l *Store) verifyDeployment(path string, deployment Deployment) error {
	if deploymentMode(deployment) == DeploymentSymlink {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return errors.New("managed symlink was replaced; existing content was preserved")
		}
		destination, err := os.Readlink(path)
		if err != nil {
			return err
		}
		expected := filepath.Join(l.root, "objects", deployment.ContentHash)
		if destination != expected {
			return errors.New("managed symlink was changed; existing content was preserved")
		}
		actual, err := HashDir(expected)
		if err != nil {
			return err
		}
		if actual != deployment.ContentHash {
			return errors.New("linked skill content is damaged; existing content was preserved")
		}
		return nil
	}
	actual, err := HashDir(path)
	if err != nil {
		return err
	}
	if actual != deployment.ContentHash {
		return errors.New("locally modified; existing content was preserved")
	}
	return nil
}

func (l *Store) planDirectory(state State, target Target, path string) (DirectoryPlan, error) {
	plan := DirectoryPlan{TargetID: target.ID, Path: path, Add: []string{}, Update: []string{}, Remove: []string{}, Conflicts: []string{}, BlockingConflicts: []string{}, Overwrite: []string{}}
	conflict := func(message, overwriteName string) {
		plan.Conflicts = append(plan.Conflicts, message)
		if overwriteName == "" {
			plan.BlockingConflicts = append(plan.BlockingConflicts, message)
		} else {
			plan.Overwrite = append(plan.Overwrite, filepath.Join(path, overwriteName))
		}
	}
	if err := l.checkTargetPath(path, true); err != nil {
		return plan, err
	}
	directory, err := os.Open(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return plan, err
	}
	entries := []os.DirEntry{}
	if err == nil {
		entries, err = readReviewEntries(directory, maxApplyFingerprintEntries)
		closeErr := directory.Close()
		if err != nil {
			return plan, err
		}
		if closeErr != nil {
			return plan, closeErr
		}
	}
	// Keep every spelling on case-sensitive filesystems, so forcing a case
	// collision never leaves a second skill with the same folded name behind.
	existing := map[string][]string{}
	for _, entry := range entries {
		fold := strings.ToLower(entry.Name())
		existing[fold] = append(existing[fold], entry.Name())
	}
	// Bound local content before verifyDeployment hashes any managed copies.
	plan.Fingerprint, err = applyDirectoryFingerprint(state, target, path, existing)
	if err != nil {
		return plan, err
	}
	managed := map[string]Deployment{}
	modified := map[string]bool{}
	checkedObjects := map[string]bool{}
	checkObject := func(name, hash string) error {
		if checkedObjects[hash] {
			return nil
		}
		actual, err := HashDir(filepath.Join(l.root, "objects", hash))
		if err != nil {
			return fmt.Errorf("cannot read skill %s: %w", name, err)
		}
		if actual != hash {
			return fmt.Errorf("stored content is damaged: %s", name)
		}
		checkedObjects[hash] = true
		return nil
	}
	for _, deployment := range target.Deployments {
		if deploymentPath(target, deployment) != path {
			continue
		}
		managed[deployment.Name] = deployment
		if err := checkObject(deployment.Name, deployment.ContentHash); err != nil {
			return plan, err
		}
		actualNames := existing[strings.ToLower(deployment.Name)]
		for _, name := range actualNames {
			if name != deployment.Name {
				conflict(deployment.Name+": existing directory has a conflicting name ("+name+")", name)
				modified[deployment.Name] = true
			}
		}
		// Consult the directory listing rather than Lstat for exact casing: on a
		// case-insensitive volume Lstat would silently accept a renamed entry.
		if !slices.Contains(actualNames, deployment.Name) {
			continue
		}
		deployed := filepath.Join(path, deployment.Name)
		if _, statErr := os.Lstat(deployed); errors.Is(statErr, os.ErrNotExist) {
			continue
		} else if statErr != nil {
			conflict(deployment.Name+": "+statErr.Error(), "")
			continue
		}
		if err = l.verifyDeployment(deployed, deployment); err != nil {
			conflict(deployment.Name+": "+err.Error(), deployment.Name)
			modified[deployment.Name] = true
		}
	}
	desired := map[string]Skill{}
	folded := map[string]string{}
	skills := []Skill{}
	if slices.Contains(targetPaths(target), path) {
		skills = desiredSkills(state, target)
	}
	for _, skill := range skills {
		fold := strings.ToLower(skill.Name)
		if other, found := folded[fold]; found {
			conflict(skill.Name+": multiple skills have the same deployment name ("+other+")", "")
			continue
		}
		folded[fold], desired[skill.Name] = skill.ID, skill
		if err := checkObject(skill.Name, skill.ContentHash); err != nil {
			return plan, err
		}
		if existingNames := existing[fold]; len(existingNames) > 0 {
			for _, existingName := range existingNames {
				if existingName != skill.Name {
					conflict(skill.Name+": existing directory has a conflicting name ("+existingName+")", existingName)
				}
			}
			old, owned := managed[skill.Name]
			if !owned && slices.Contains(existingNames, skill.Name) {
				conflict(skill.Name+": existing content is not managed by racomi", skill.Name)
			}
			if !owned || modified[skill.Name] || len(existingNames) != 1 || existingNames[0] != skill.Name || old.SkillID != skill.ID || old.ContentHash != skill.ContentHash || deploymentMode(old) != state.Settings.DeploymentMode {
				plan.Update = append(plan.Update, skill.Name)
			}
		} else {
			plan.Add = append(plan.Add, skill.Name)
		}
	}
	for name := range managed {
		if _, required := desired[name]; !required {
			plan.Remove = append(plan.Remove, name)
		}
	}
	sort.Strings(plan.Add)
	sort.Strings(plan.Update)
	sort.Strings(plan.Remove)
	plan.Conflicts = unique(plan.Conflicts)
	plan.BlockingConflicts, plan.Overwrite = unique(plan.BlockingConflicts), unique(plan.Overwrite)
	sort.Strings(plan.Conflicts)
	sort.Strings(plan.BlockingConflicts)
	sort.Strings(plan.Overwrite)
	return plan, nil
}

const (
	maxApplyFingerprintBytes   int64 = 1 << 30
	maxApplyFingerprintEntries       = 100_000
)

func applyDirectoryFingerprint(state State, target Target, path string, existing map[string][]string) (string, error) {
	// Include desired and formerly owned names plus every current spelling of
	// those names. Unrelated user entries do not need to be read or changed.
	names := []string{}
	addName := func(name string) {
		if actualNames := existing[strings.ToLower(name)]; len(actualNames) > 0 {
			names = append(names, actualNames...)
		} else {
			names = append(names, name)
		}
	}
	deployments := []Deployment{}
	for _, deployment := range target.Deployments {
		if deploymentPath(target, deployment) == path {
			addName(deployment.Name)
			deployments = append(deployments, deployment)
		}
	}
	skills := []Skill{}
	if slices.Contains(targetPaths(target), path) {
		skills = desiredSkills(state, target)
	}
	for _, skill := range skills {
		addName(skill.Name)
	}
	content, err := deploymentFingerprint(path, unique(names))
	if err != nil {
		return "", err
	}
	// A deployment name alone does not identify what the user approved. Bind
	// versions, identities, mode and prior ownership as well as local content.
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "content:%q;mode:%q;", content, state.Settings.DeploymentMode)
	sort.Slice(skills, func(i, j int) bool { return skills[i].ID < skills[j].ID })
	for _, skill := range skills {
		_, _ = fmt.Fprintf(h, "desired:%q,%q,%q;", skill.ID, skill.Name, skill.ContentHash)
	}
	sort.Slice(deployments, func(i, j int) bool { return deployments[i].Name < deployments[j].Name })
	for _, deployment := range deployments {
		_, _ = fmt.Fprintf(h, "owned:%q,%q,%q,%q;", deployment.SkillID, deployment.Name, deployment.ContentHash, deploymentMode(deployment))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func readReviewEntries(directory *os.File, remaining int) ([]os.DirEntry, error) {
	entries, err := directory.ReadDir(remaining + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(entries) > remaining {
		return nil, errors.New("target content exceeds the 100,000 entry review limit")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return entries, nil
}

// deploymentFingerprint reads only immediate deployment entries, using rooted
// operations and never following links. Bounds also apply to unmanaged content
// that would otherwise be overwritten, including files that grow while read.
func deploymentFingerprint(path string, names []string) (string, error) {
	h := sha256.New()
	writeString := func(value string) {
		_ = binary.Write(h, binary.BigEndian, uint64(len(value)))
		_, _ = io.WriteString(h, value)
	}
	root, err := os.OpenRoot(path)
	if errors.Is(err, os.ErrNotExist) {
		writeString("missing target")
		return hex.EncodeToString(h.Sum(nil)), nil
	}
	if err != nil {
		return "", err
	}
	defer root.Close()
	writeString("existing target")
	sort.Strings(names)
	var bytes int64
	entries := 0
	readEntry := func(name string) error {
		entries++
		if entries > maxApplyFingerprintEntries {
			return errors.New("target content exceeds the 100,000 entry review limit")
		}
		writeString(name)
		info, err := root.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			writeString("missing entry")
			return nil
		}
		if err != nil {
			return err
		}
		_ = binary.Write(h, binary.BigEndian, uint32(info.Mode()))
		if info.Mode()&os.ModeSymlink != 0 {
			destination, err := root.Readlink(name)
			if err != nil {
				return err
			}
			writeString(destination)
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported target file: %s", filepath.Join(path, name))
		}
		if info.Size() > maxApplyFingerprintBytes-bytes {
			return errors.New("target content exceeds the 1 GiB review limit")
		}
		_ = binary.Write(h, binary.BigEndian, uint64(info.Size()))
		file, err := root.Open(name)
		if err != nil {
			return err
		}
		defer file.Close()
		current, err := file.Stat()
		if err != nil {
			return err
		}
		if !current.Mode().IsRegular() || !os.SameFile(info, current) {
			return errors.New("target contents changed while preparing review")
		}
		n, err := io.Copy(h, io.LimitReader(file, maxApplyFingerprintBytes-bytes+1))
		bytes += n
		if err != nil {
			return err
		}
		if bytes > maxApplyFingerprintBytes {
			return errors.New("target content exceeds the 1 GiB review limit")
		}
		if n != info.Size() {
			return errors.New("target contents changed while preparing review")
		}
		return nil
	}
	var walk func(string) error
	walk = func(name string) error {
		info, err := root.Lstat(name)
		if errors.Is(err, os.ErrNotExist) || (err == nil && !info.IsDir()) {
			return readEntry(name)
		}
		if err != nil {
			return err
		}
		if err = readEntry(name); err != nil {
			return err
		}
		directory, err := root.Open(name)
		if err != nil {
			return err
		}
		current, statErr := directory.Stat()
		if statErr != nil || !current.IsDir() || !os.SameFile(info, current) {
			_ = directory.Close()
			return errors.New("target contents changed while preparing review")
		}
		children, readErr := readReviewEntries(directory, maxApplyFingerprintEntries-entries)
		closeErr := directory.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		for _, child := range children {
			if err = walk(filepath.Join(name, child.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	for _, name := range names {
		if err = walk(name); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// A directoryTransaction stages new entries and keeps old entries until the
// index for every destination commits. Root-relative renames do not follow links.
type directoryTransaction struct {
	plan          DirectoryPlan
	parent        *os.Root
	stage         string
	base          string
	stageBase     string
	changed       []string
	backedUp      []string
	installed     []string
	createdTarget bool
	keepStage     bool
	recheck       func() error
}

func (transaction *directoryTransaction) cleanup() {
	if transaction.parent != nil {
		transaction.parent.Close()
	}
	if transaction.stage != "" && !transaction.keepStage {
		_ = os.RemoveAll(transaction.stage)
	}
}

func (l *Store) stageDirectory(state State, target Target, plan DirectoryPlan) (*directoryTransaction, error) {
	transaction := &directoryTransaction{plan: plan, base: filepath.Base(plan.Path), changed: append(append([]string{}, plan.Add...), plan.Update...)}
	transaction.recheck = func() error {
		current, err := l.planDirectory(state, target, plan.Path)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(plan, current) {
			return errors.New("target contents changed while staging; preview and apply again")
		}
		return nil
	}
	// Releasing ownership of a missing directory needs no filesystem changes.
	if _, err := os.Lstat(plan.Path); errors.Is(err, os.ErrNotExist) && len(transaction.changed) == 0 {
		return transaction, nil
	}
	parent := filepath.Dir(plan.Path)
	if err := noSymlinks(parent); err != nil {
		return transaction, err
	}
	if err := os.MkdirAll(parent, 0755); err != nil {
		return transaction, err
	}
	if err := noSymlinks(parent); err != nil {
		return transaction, err
	}
	var err error
	transaction.parent, err = os.OpenRoot(parent)
	if err != nil {
		return transaction, err
	}
	transaction.stage, err = os.MkdirTemp(parent, ".racomi-apply-")
	if err != nil {
		return transaction, err
	}
	transaction.stageBase = filepath.Base(transaction.stage)
	for _, name := range []string{"new", "old"} {
		if err = os.Mkdir(filepath.Join(transaction.stage, name), 0700); err != nil {
			return transaction, err
		}
	}
	for _, skill := range desiredSkills(state, target) {
		if !slices.Contains(transaction.changed, skill.Name) {
			continue
		}
		object := filepath.Join(l.root, "objects", skill.ContentHash)
		destination := filepath.Join(transaction.stage, "new", skill.Name)
		if state.Settings.DeploymentMode == DeploymentSymlink {
			err = os.Symlink(object, destination)
		} else {
			err = CopyDir(object, destination)
		}
		if err != nil {
			return transaction, err
		}
		if err = l.verifyDeployment(destination, Deployment{ContentHash: skill.ContentHash, Mode: state.Settings.DeploymentMode}); err != nil {
			return transaction, err
		}
	}
	return transaction, nil
}

func (transaction *directoryTransaction) commit() error {
	if transaction.recheck != nil {
		if err := transaction.recheck(); err != nil {
			return err
		}
	}
	if transaction.parent == nil {
		return nil
	}
	if info, err := transaction.parent.Lstat(transaction.base); errors.Is(err, os.ErrNotExist) {
		if err = transaction.parent.Mkdir(transaction.base, 0755); err != nil {
			return err
		}
		transaction.createdTarget = true
	} else if err != nil {
		return err
	} else if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("target is no longer a regular directory")
	}
	affected := unique(append(append([]string{}, transaction.changed...), transaction.plan.Remove...))
	for _, path := range transaction.plan.Overwrite {
		if filepath.Dir(path) != transaction.plan.Path {
			return errors.New("overwrite path does not belong to the target")
		}
		affected = append(affected, filepath.Base(path))
	}
	// Use the actual spelling from the directory, rather than the desired name.
	// This preserves original casing when a later destination or index fails.
	directory, err := transaction.parent.Open(transaction.base)
	if err != nil {
		return err
	}
	entries, readErr := directory.ReadDir(maxApplyFingerprintEntries + 1)
	closeErr := directory.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	if len(entries) > maxApplyFingerprintEntries {
		return errors.New("target content exceeds the 100,000 entry review limit")
	}
	backup := []string{}
	for _, entry := range entries {
		if slices.Contains(affected, entry.Name()) {
			backup = append(backup, entry.Name())
			continue
		}
		if slices.ContainsFunc(affected, func(name string) bool { return strings.EqualFold(name, entry.Name()) }) {
			return errors.New("target contents changed while staging; preview and apply again")
		}
	}
	sort.Strings(backup)
	for _, name := range backup {
		path := filepath.Join(transaction.base, name)
		if _, err := transaction.parent.Lstat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if err := transaction.parent.Rename(path, filepath.Join(transaction.stageBase, "old", name)); err != nil {
			return err
		}
		transaction.backedUp = append(transaction.backedUp, name)
	}
	for _, name := range transaction.changed {
		path := filepath.Join(transaction.base, name)
		if _, err := transaction.parent.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("destination unexpectedly appeared: %s", name)
		}
		if err := transaction.parent.Rename(filepath.Join(transaction.stageBase, "new", name), path); err != nil {
			return err
		}
		transaction.installed = append(transaction.installed, name)
	}
	return nil
}

func (transaction *directoryTransaction) rollback() error {
	if transaction.parent == nil {
		return nil
	}
	failures := []error{}
	for i := len(transaction.installed) - 1; i >= 0; i-- {
		if err := transaction.parent.RemoveAll(filepath.Join(transaction.base, transaction.installed[i])); err != nil {
			failures = append(failures, err)
		}
	}
	for i := len(transaction.backedUp) - 1; i >= 0; i-- {
		name := transaction.backedUp[i]
		if _, err := transaction.parent.Lstat(filepath.Join(transaction.base, name)); !errors.Is(err, os.ErrNotExist) {
			failures = append(failures, fmt.Errorf("cannot restore %s: destination unexpectedly exists", name))
			continue
		}
		if err := transaction.parent.Rename(filepath.Join(transaction.stageBase, "old", name), filepath.Join(transaction.base, name)); err != nil {
			failures = append(failures, err)
		}
	}
	if transaction.createdTarget && len(failures) == 0 {
		if err := transaction.parent.Remove(transaction.base); err != nil {
			failures = append(failures, err)
		}
	}
	if len(failures) > 0 {
		transaction.keepStage = true
		return fmt.Errorf("rollback needs attention; old content retained at %s: %w", transaction.stage, errors.Join(failures...))
	}
	return nil
}

func (l *Store) Apply(targetID string) error { return l.ApplyTargets([]string{targetID}) }

// ForceApply replaces only conflicts explicitly classified as overwritable.
// UI callers should use ForceApplyReviewed to bind this action to their review.
func (l *Store) ForceApply(targetID string) error { return l.ForceApplyTargets([]string{targetID}) }

func (l *Store) ForceApplyTargets(targetIDs []string) error {
	return l.applyTargets(targetIDs, true, nil)
}

func (l *Store) ForceApplyReviewed(targetID string, reviewed ApplyPlan) error {
	return l.ForceApplyTargetsReviewed([]string{targetID}, reviewed)
}

func (l *Store) ForceApplyTargetsReviewed(targetIDs []string, reviewed ApplyPlan) error {
	return l.applyTargets(targetIDs, true, &reviewed)
}

// ApplyTargets stages and checks every destination before modifying any. A
// failed rename or index write rolls all completed destinations back together.
func (l *Store) ApplyTargets(targetIDs []string) error {
	return l.applyTargets(targetIDs, false, nil)
}

func (l *Store) applyTargets(targetIDs []string, force bool, reviewed *ApplyPlan) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	state, err := l.readState()
	if err != nil {
		return err
	}
	targetIDs = unique(targetIDs)
	plan, err := l.planTargets(state, targetIDs)
	if err != nil {
		return err
	}
	if reviewed != nil && !reflect.DeepEqual(plan.Directories, reviewed.Directories) {
		return errors.New("target contents changed since review; preview and apply again")
	}
	if conflicts := applyConflicts(plan, force); len(conflicts) > 0 {
		return fmt.Errorf("application has conflicts: %s", strings.Join(conflicts, "; "))
	}
	if len(plan.Add)+len(plan.Update)+len(plan.Remove) == 0 {
		return nil
	}
	next := cloneState(state)
	for i, target := range next.Targets {
		if !slices.Contains(targetIDs, target.ID) {
			continue
		}
		next.Targets[i].Deployments = []Deployment{}
		for _, path := range targetPaths(target) {
			for _, skill := range desiredSkills(state, target) {
				next.Targets[i].Deployments = append(next.Targets[i].Deployments, Deployment{Path: path, Mode: state.Settings.DeploymentMode, SkillID: skill.ID, Name: skill.Name, ContentHash: skill.ContentHash})
			}
		}
	}
	if _, err = l.snapshot(state, "before applying targets"); err != nil {
		return err
	}
	transactions := []*directoryTransaction{}
	defer func() {
		for _, transaction := range transactions {
			transaction.cleanup()
		}
	}()
	for _, directory := range plan.Directories {
		if len(directory.Add)+len(directory.Update)+len(directory.Remove) == 0 {
			continue
		}
		index := slices.IndexFunc(state.Targets, func(t Target) bool { return t.ID == directory.TargetID })
		transaction, stageErr := l.stageDirectory(state, state.Targets[index], directory)
		transactions = append(transactions, transaction)
		if stageErr != nil {
			return stageErr
		}
	}
	// Detect local edits made while the potentially large copies were staged.
	checked, err := l.planTargets(state, targetIDs)
	if err != nil {
		return err
	}
	if conflicts := applyConflicts(checked, force); len(conflicts) > 0 {
		return fmt.Errorf("application has conflicts: %s", strings.Join(conflicts, "; "))
	}
	if !reflect.DeepEqual(plan.Directories, checked.Directories) {
		return errors.New("target contents changed while staging; preview and apply again")
	}
	rollback := func(cause error) error {
		failures := []error{cause}
		for i := len(transactions) - 1; i >= 0; i-- {
			if rollbackErr := transactions[i].rollback(); rollbackErr != nil {
				failures = append(failures, rollbackErr)
			}
		}
		return errors.Join(failures...)
	}
	for _, transaction := range transactions {
		if err = transaction.commit(); err != nil {
			return rollback(err)
		}
	}
	if err = l.writeState(next); err != nil {
		return rollback(err)
	}
	return nil
}

func applyConflicts(plan ApplyPlan, force bool) []string {
	if force {
		return plan.BlockingConflicts
	}
	return plan.Conflicts
}
