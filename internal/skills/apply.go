package skills

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
)

func emptyPlan(targetID string) ApplyPlan {
	return ApplyPlan{TargetID: targetID, Add: []string{}, Update: []string{}, Remove: []string{}, Conflicts: []string{}, Directories: []DirectoryPlan{}}
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
		for _, conflict := range directory.Conflicts {
			if len(plan.Directories) == 1 {
				plan.Conflicts = append(plan.Conflicts, conflict)
			} else {
				plan.Conflicts = append(plan.Conflicts, directory.Path+": "+conflict)
			}
		}
	}
	plan.Add, plan.Update, plan.Remove, plan.Conflicts = unique(plan.Add), unique(plan.Update), unique(plan.Remove), unique(plan.Conflicts)
	sort.Strings(plan.Add)
	sort.Strings(plan.Update)
	sort.Strings(plan.Remove)
	sort.Strings(plan.Conflicts)
}

func (l *Store) plan(state State, target Target) (ApplyPlan, error) {
	plan := emptyPlan(target.ID)
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
	plan := DirectoryPlan{TargetID: target.ID, Path: path, Add: []string{}, Update: []string{}, Remove: []string{}, Conflicts: []string{}}
	if err := l.checkTargetPath(path, true); err != nil {
		return plan, err
	}
	entries, err := os.ReadDir(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return plan, err
	}
	existing := map[string]string{}
	for _, entry := range entries {
		existing[strings.ToLower(entry.Name())] = entry.Name()
	}
	managed := map[string]Deployment{}
	for _, deployment := range target.Deployments {
		if deploymentPath(target, deployment) != path {
			continue
		}
		managed[deployment.Name] = deployment
		deployed := filepath.Join(path, deployment.Name)
		if _, statErr := os.Lstat(deployed); errors.Is(statErr, os.ErrNotExist) {
			continue
		} else if statErr != nil {
			plan.Conflicts = append(plan.Conflicts, deployment.Name+": "+statErr.Error())
			continue
		}
		if err = l.verifyDeployment(deployed, deployment); err != nil {
			plan.Conflicts = append(plan.Conflicts, deployment.Name+": "+err.Error())
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
			plan.Conflicts = append(plan.Conflicts, skill.Name+": multiple skills have the same deployment name ("+other+")")
			continue
		}
		folded[fold], desired[skill.Name] = skill.ID, skill
		actual, err := HashDir(filepath.Join(l.root, "objects", skill.ContentHash))
		if err != nil {
			return plan, fmt.Errorf("cannot read skill %s: %w", skill.Name, err)
		}
		if actual != skill.ContentHash {
			return plan, fmt.Errorf("stored content is damaged: %s", skill.Name)
		}
		if existingName, found := existing[fold]; found {
			if existingName != skill.Name {
				plan.Conflicts = append(plan.Conflicts, skill.Name+": existing directory has a conflicting name ("+existingName+")")
				continue
			}
			old, owned := managed[skill.Name]
			if !owned {
				plan.Conflicts = append(plan.Conflicts, skill.Name+": existing content is not managed by racomi")
				continue
			}
			if old.SkillID != skill.ID || old.ContentHash != skill.ContentHash || deploymentMode(old) != state.Settings.DeploymentMode {
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
	sort.Strings(plan.Conflicts)
	return plan, nil
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
	sort.Strings(affected)
	for _, name := range affected {
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

// ApplyTargets stages and checks every destination before modifying any. A
// failed rename or index write rolls all completed destinations back together.
func (l *Store) ApplyTargets(targetIDs []string) error {
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
	if len(plan.Conflicts) > 0 {
		return fmt.Errorf("application has conflicts: %s", strings.Join(plan.Conflicts, "; "))
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
	if len(checked.Conflicts) > 0 {
		return fmt.Errorf("application has conflicts: %s", strings.Join(checked.Conflicts, "; "))
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
