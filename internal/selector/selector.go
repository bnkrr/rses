package selector

import (
	"fmt"
	"path/filepath"

	"github.com/bnkrr/rses/internal/session"
)

func Select(specs []session.Spec, names []string, group string, tags []string, includeRemote bool, host string) []session.Spec {
	var out []session.Spec
	for _, spec := range specs {
		if spec.IsRemote() && !includeRemote {
			continue
		}
		if spec.MatchesNames(names) && spec.MatchesGroup(group) && spec.MatchesTags(tags) && spec.MatchesHost(host) {
			out = append(out, spec)
		}
	}
	return out
}

func SelectStartStop(specs []session.Spec, names []string, group string, tags []string, all bool, action string, includeRemote bool, host string) ([]session.Spec, error) {
	hasSelector := len(names) > 0 || group != "" || len(tags) > 0
	if all && hasSelector {
		return nil, fmt.Errorf("use either --all or specific selectors with %s, not both", action)
	}
	if !all && !hasSelector {
		return nil, fmt.Errorf("refusing to %s all sessions without --all, a name, --group, or --tag", action)
	}
	selected := Select(specs, names, group, tags, includeRemote, host)
	if len(selected) == 0 {
		return nil, fmt.Errorf("no sessions matched")
	}
	return selected, nil
}

func SelectStartStopOptional(specs []session.Spec, names []string, group string, tags []string, all bool, hasConfig bool, action string, includeRemote bool, host string) ([]session.Spec, error) {
	hasSelector := len(names) > 0 || group != "" || len(tags) > 0
	if all && hasSelector {
		return nil, fmt.Errorf("use either --all or specific selectors with %s, not both", action)
	}
	if !all && !hasSelector {
		return nil, fmt.Errorf("refusing to %s all sessions without --all, a name, --group, or --tag", action)
	}
	if !hasConfig {
		return nil, nil
	}
	selected := Select(specs, names, group, tags, includeRemote, host)
	if len(selected) == 0 && !hasSelector {
		return nil, fmt.Errorf("no sessions matched")
	}
	return selected, nil
}

func ExpandStartDependencies(targets, all []session.Spec, includeRemote bool, host string) ([]session.Spec, error) {
	byName := specsByName(all)
	seen := map[string]bool{}
	visiting := map[string]bool{}
	var out []session.Spec
	var visit func(session.Spec) error
	visit = func(spec session.Spec) error {
		if seen[spec.Name] {
			return nil
		}
		if visiting[spec.Name] {
			return fmt.Errorf("dependency cycle includes %q", spec.Name)
		}
		visiting[spec.Name] = true
		for _, depName := range spec.DependsOn {
			dep, ok := byName[depName]
			if !ok {
				return fmt.Errorf("session %q depends on unknown session %q", spec.Name, depName)
			}
			if dep.IsRemote() && !includeRemote {
				return fmt.Errorf("session %q depends on remote session %q; add --remote", spec.Name, depName)
			}
			if dep.IsRemote() && !dep.MatchesHost(host) {
				return fmt.Errorf("session %q depends on remote session %q on host %q, outside --host %q", spec.Name, depName, dep.Host, host)
			}
			if err := visit(dep); err != nil {
				return err
			}
		}
		visiting[spec.Name] = false
		seen[spec.Name] = true
		out = append(out, spec)
		return nil
	}
	for _, spec := range targets {
		if spec.IsRemote() && !includeRemote {
			return nil, fmt.Errorf("remote session %q requires --remote", spec.Name)
		}
		if err := visit(spec); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func AppendStopDependencies(targets, all []session.Spec, includeRemote bool, host string) ([]session.Spec, error) {
	byName := specsByName(all)
	out := append([]session.Spec{}, targets...)
	seen := map[string]bool{}
	for _, spec := range out {
		seen[spec.Name] = true
		if spec.IsRemote() && !includeRemote {
			return nil, fmt.Errorf("remote session %q requires --remote", spec.Name)
		}
	}
	for _, spec := range targets {
		for _, depName := range spec.StopDependencies {
			if seen[depName] {
				continue
			}
			dep, ok := byName[depName]
			if !ok {
				return nil, fmt.Errorf("session %q stop_dependencies includes unknown session %q", spec.Name, depName)
			}
			if dep.IsRemote() && !includeRemote {
				return nil, fmt.Errorf("session %q stop_dependencies includes remote session %q; add --remote", spec.Name, depName)
			}
			if dep.IsRemote() && !dep.MatchesHost(host) {
				return nil, fmt.Errorf("session %q stop_dependencies includes remote session %q on host %q, outside --host %q", spec.Name, depName, dep.Host, host)
			}
			seen[dep.Name] = true
			out = append(out, dep)
		}
	}
	return out, nil
}

func ProjectDirForRequest(projectDir string, specs []session.Spec, hasConfig bool, names []string, group string, tags []string, all bool) string {
	if !hasConfig {
		return ""
	}
	hasSelector := len(names) > 0 || group != "" || len(tags) > 0 || all
	if !hasSelector || len(specs) > 0 {
		return filepath.Clean(projectDir)
	}
	return ""
}

func NamesForRequest(names []string, all bool) []string {
	if all {
		return nil
	}
	return names
}

func specsByName(specs []session.Spec) map[string]session.Spec {
	out := make(map[string]session.Spec, len(specs))
	for _, spec := range specs {
		out[spec.Name] = spec
	}
	return out
}
