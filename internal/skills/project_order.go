package skills

import (
	"errors"
	"reflect"
	"slices"
	"time"
)

// Target timestamps describe local configuration, not deployment activity.
// Ignore client-supplied timestamps and do not refresh unchanged saves.
func stampTarget(target *Target, previous *Target) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if previous == nil {
		target.CreatedAt, target.UpdatedAt = now, now
		return
	}
	target.CreatedAt, target.UpdatedAt = previous.CreatedAt, previous.UpdatedAt
	left, right := *target, *previous
	left.CreatedAt, left.UpdatedAt, right.CreatedAt, right.UpdatedAt = "", "", "", ""
	left.Deployments, right.Deployments = nil, nil
	left.Pending, right.Pending = false, false
	if !reflect.DeepEqual(left, right) {
		target.UpdatedAt = now
	}
}

// ReorderProjects persists a complete project order on this device. Global
// destinations retain their slots, paths and deployment ownership.
func (l *Store) ReorderProjects(projectIDs []string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	state, err := l.readState()
	if err != nil {
		return err
	}
	projects := map[string]Target{}
	previous := []string{}
	for _, target := range state.Targets {
		if target.Kind == "project" {
			projects[target.ID] = target
			previous = append(previous, target.ID)
		}
	}
	if len(projectIDs) != len(projects) {
		return errors.New("project order must include every project exactly once")
	}
	seen := map[string]bool{}
	for _, projectID := range projectIDs {
		if _, ok := projects[projectID]; !ok || seen[projectID] {
			return errors.New("project order must include every project exactly once")
		}
		seen[projectID] = true
	}
	if slices.Equal(previous, projectIDs) {
		return nil
	}
	for i, next := 0, 0; i < len(state.Targets); i++ {
		if state.Targets[i].Kind == "project" {
			state.Targets[i] = projects[projectIDs[next]]
			next++
		}
	}
	return l.writeState(state)
}
