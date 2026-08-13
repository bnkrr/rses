package cli

import (
	"os"

	"github.com/bnkrr/rses/internal/config"
	"github.com/bnkrr/rses/internal/session"
)

var configDirectory string

func locateConfig(startDir string) (config.Source, error) {
	if configDirectory != "" {
		return config.LocateDir(configDirectory)
	}
	return config.Locate(startDir)
}

func load() (string, []session.Spec, session.RuntimeOptions, error) {
	startDir, err := os.Getwd()
	if err != nil {
		return "", nil, session.RuntimeOptions{}, err
	}
	source, err := locateConfig(startDir)
	if err != nil {
		return "", nil, session.RuntimeOptions{}, err
	}
	specs, runtime, err := config.LoadFileWithRuntime(source.Root, source.Path)
	if err != nil {
		return "", nil, session.RuntimeOptions{}, err
	}
	return source.Root, specs, runtime, nil
}

func loadOptional() (string, []session.Spec, session.RuntimeOptions, bool, error) {
	startDir, err := os.Getwd()
	if err != nil {
		return "", nil, session.RuntimeOptions{}, false, err
	}
	source, err := locateConfig(startDir)
	if err != nil {
		if configDirectory != "" {
			return "", nil, session.RuntimeOptions{}, false, err
		}
		return "", nil, session.RuntimeOptions{}, false, nil
	}
	specs, runtime, err := config.LoadFileWithRuntime(source.Root, source.Path)
	if err != nil {
		return "", nil, session.RuntimeOptions{}, false, err
	}
	return source.Root, specs, runtime, true, nil
}

func specsFromStatuses(statuses []session.Status) []session.Spec {
	specs := make([]session.Spec, 0, len(statuses))
	for _, st := range statuses {
		specs = append(specs, session.Spec{
			Name:    st.Name,
			Group:   st.Group,
			Tags:    st.Tags,
			Summary: st.Summary,
		})
	}
	return specs
}
