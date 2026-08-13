package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/bnkrr/rses/internal/paths"
	"github.com/bnkrr/rses/internal/runtime/tmux"
	"github.com/bnkrr/rses/internal/session"
)

var recordMu sync.Mutex

func matchingRecords(req Request) ([]tmux.RuntimeRecord, error) {
	records, err := loadRecords()
	if err != nil {
		return nil, err
	}
	out := make([]tmux.RuntimeRecord, 0, len(records))
	for _, record := range records {
		if record.Kind == "ssh-tmux" {
			continue
		}
		if req.ProjectDir != "" && filepath.Clean(req.ProjectDir) != filepath.Clean(record.ProjectDir) {
			continue
		}
		spec := specFromRecord(record)
		if !spec.MatchesNames(req.Names) || !spec.MatchesGroup(req.Group) || !spec.MatchesTags(req.Tags) {
			continue
		}
		out = append(out, record)
	}
	if req.ProjectDir == "" && len(req.Names) > 0 {
		if err := rejectAmbiguousRecords(req, out); err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].ProjectDir < out[j].ProjectDir
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func loadRecords() ([]tmux.RuntimeRecord, error) {
	recordMu.Lock()
	defer recordMu.Unlock()
	root := filepath.Join(paths.StateDir(), "scopes")
	var records []tmux.RuntimeRecord
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() || entry.Name() != "record.json" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		var record tmux.RuntimeRecord
		if err := json.Unmarshal(data, &record); err != nil {
			return nil
		}
		if record.Name != "" && record.ProjectDir != "" {
			records = append(records, record)
		}
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return records, err
}

func rejectAmbiguousRecords(req Request, records []tmux.RuntimeRecord) error {
	for _, name := range req.Names {
		roots := map[string]bool{}
		for _, record := range records {
			if record.Name == name {
				roots[record.ProjectDir] = true
			}
		}
		if len(roots) <= 1 {
			continue
		}
		values := make([]string, 0, len(roots))
		for root := range roots {
			values = append(values, root)
		}
		sort.Strings(values)
		return fmt.Errorf("session name %q is ambiguous across config roots:\n  %s\nRun from the matching config directory or set RSES_CONFIG", name, strings.Join(values, "\n  "))
	}
	return nil
}

func recordKey(name, projectDir string) string {
	return filepath.Clean(projectDir) + "\x00" + name
}

func sortStatuses(statuses []session.Status) {
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].Name < statuses[j].Name })
}
