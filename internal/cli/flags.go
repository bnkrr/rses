package cli

import (
	"flag"
	"fmt"
	"strings"
)

type tagValues []string

func (v *tagValues) String() string {
	return strings.Join(*v, ",")
}

func (v *tagValues) Set(value string) error {
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			return fmt.Errorf("tag cannot be empty")
		}
		*v = append(*v, item)
	}
	return nil
}

func (v tagValues) Values() []string {
	return append([]string(nil), v...)
}

func (v tagValues) HasAny() bool {
	return len(v) > 0
}

func parseInterspersed(fs *flag.FlagSet, args []string, valueFlags ...string) error {
	takesValue := map[string]bool{}
	for _, name := range valueFlags {
		takesValue[name] = true
	}
	var flags []string
	var positionals []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positionals = append(positionals, arg)
			continue
		}
		flags = append(flags, arg)
		name := strings.TrimLeft(arg, "-")
		if idx := strings.IndexByte(name, '='); idx >= 0 {
			name = name[:idx]
		}
		if takesValue[name] && !strings.Contains(arg, "=") && i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	return fs.Parse(append(flags, positionals...))
}
