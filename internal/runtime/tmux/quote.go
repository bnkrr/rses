package tmux

import "strings"

func CommandLine(argv []string) string {
	parts := make([]string, 0, len(argv))
	for _, arg := range argv {
		parts = append(parts, ShellQuote(arg))
	}
	return strings.Join(parts, " ")
}

func ShellQuotePath(path string) string {
	if strings.HasPrefix(path, "~/") {
		return "~/" + ShellQuote(strings.TrimPrefix(path, "~/"))
	}
	if strings.HasPrefix(path, "$HOME/") {
		return "$HOME/" + ShellQuote(strings.TrimPrefix(path, "$HOME/"))
	}
	return ShellQuote(path)
}

func ShellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
