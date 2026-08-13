package tablefmt

import (
	"fmt"
	"strings"

	"github.com/mattn/go-runewidth"
)

const ellipsis = "..."

func CompactTags(tags []string, max int) string {
	if len(tags) == 0 {
		return ""
	}
	if max <= 0 {
		return fmt.Sprintf("+%d", len(tags))
	}
	if len(tags) <= max {
		return strings.Join(tags, ",")
	}
	return strings.Join(tags[:max], ",") + fmt.Sprintf(",+%d", len(tags)-max)
}

func Truncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if runewidth.StringWidth(value) <= width {
		return value
	}
	if width <= len(ellipsis) {
		return strings.Repeat(".", width)
	}

	return runewidth.Truncate(value, width, ellipsis)
}

func PadRight(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if runewidth.StringWidth(value) >= width {
		return value
	}
	return value + strings.Repeat(" ", width-runewidth.StringWidth(value))
}

func DisplayWidth(value string) int {
	return runewidth.StringWidth(value)
}
