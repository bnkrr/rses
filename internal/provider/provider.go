package provider

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bnkrr/rses/internal/session"
)

type Output struct {
	Command session.Command `json:"command"`
}

type Manifest struct {
	Command []string `json:"command"`
}

func GenerateCommand(root, name, provider string, with map[string]any) (session.Command, error) {
	providerDir := filepath.Join(root, "providers", provider)
	providerCommand, err := LoadCommand(providerDir)
	if err != nil {
		return session.Command{}, fmt.Errorf("session %q provider %q invalid: %w", name, provider, err)
	}

	payload := map[string]any{
		"name":     name,
		"provider": provider,
		"root":     root,
		"with":     with,
	}
	in, err := json.Marshal(payload)
	if err != nil {
		return session.Command{}, err
	}

	cmd := exec.Command(providerCommand[0], providerCommand[1:]...)
	cmd.Dir = providerDir
	cmd.Stdin = bytes.NewReader(in)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	out := stdout.Bytes()
	if err != nil {
		return session.Command{}, fmt.Errorf("provider %q for session %q failed: %w%s", provider, name, err, formatProcessOutput(out, stderr.Bytes()))
	}

	var rawOut map[string]json.RawMessage
	if err := json.Unmarshal(out, &rawOut); err != nil {
		return session.Command{}, fmt.Errorf("provider %q for session %q returned invalid output: %w%s", provider, name, err, formatProcessOutput(out, stderr.Bytes()))
	}
	for key := range rawOut {
		if key != "command" {
			return session.Command{}, fmt.Errorf("provider %q for session %q returned forbidden field %q; providers may only return command%s", provider, name, key, formatProcessOutput(out, stderr.Bytes()))
		}
	}
	if _, ok := rawOut["command"]; !ok {
		return session.Command{}, fmt.Errorf("provider %q for session %q returned no command%s", provider, name, formatProcessOutput(out, stderr.Bytes()))
	}
	var generated Output
	if err := json.Unmarshal(out, &generated); err != nil {
		return session.Command{}, fmt.Errorf("provider %q for session %q returned invalid command: %w%s", provider, name, err, formatProcessOutput(out, stderr.Bytes()))
	}
	return generated.Command, nil
}

func formatProcessOutput(stdout, stderr []byte) string {
	parts := make([]string, 0, 2)
	if text := strings.TrimSpace(string(stderr)); text != "" {
		parts = append(parts, "stderr:\n"+text)
	}
	if text := strings.TrimSpace(string(stdout)); text != "" {
		parts = append(parts, "stdout:\n"+text)
	}
	if len(parts) == 0 {
		return ""
	}
	return "\n" + strings.Join(parts, "\n")
}

func LoadCommand(providerDir string) ([]string, error) {
	info, err := os.Stat(providerDir)
	if err != nil {
		return nil, fmt.Errorf("not found at %s", providerDir)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is a file; providers must be directories containing provider.json", providerDir)
	}

	manifestPath := filepath.Join(providerDir, "provider.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", manifestPath, err)
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse %s: %w", manifestPath, err)
	}
	if len(manifest.Command) == 0 {
		return nil, fmt.Errorf("%s missing command", manifestPath)
	}
	for i, item := range manifest.Command {
		if strings.TrimSpace(item) == "" {
			return nil, fmt.Errorf("%s command[%d] is empty", manifestPath, i)
		}
	}
	return manifest.Command, nil
}
