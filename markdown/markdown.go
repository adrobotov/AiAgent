package markdown

import (
	"fmt"
	"regexp"
	"runtime"
	"strings"
)

// stripANSI removes ANSI escape codes from text
func stripANSI(text string) string {
	// Match actual ESC characters (0x1B) followed by ANSI sequences
	r := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\([a-zA-Z]`)
	text = r.ReplaceAllString(text, "")
	// Also remove literal \033 sequences (sometimes sent as text)
	r2 := regexp.MustCompile(`\\033\[[0-9;]*[a-zA-Z]`)
	text = r2.ReplaceAllString(text, "")
	return text
}

// Renderer renders markdown text for terminal display.
type Renderer struct {
	colors bool
}

// New creates a new markdown renderer.
func New(colors bool) *Renderer {
	// Disable ANSI colors on Windows (PowerShell doesn't support them well)
	if colors && runtime.GOOS == "windows" {
		colors = false
	}
	return &Renderer{colors: colors}
}

// Render formats markdown text for terminal output.
func (r *Renderer) Render(text string) string {
	if text == "" {
		return text
	}

	// Strip ANSI escape codes from input
	text = stripANSI(text)

	lines := strings.Split(text, "\n")
	var result []string

	for _, line := range lines {
		result = append(result, r.renderLine(line))
	}

	return strings.Join(result, "\n")
}

func (r *Renderer) renderLine(line string) string {
	line = strings.TrimSpace(line)

	// Code blocks
	if strings.HasPrefix(line, "```") {
		if r.colors {
			return fmt.Sprintf("\033[90m%s\033[0m", line)
		}
		return "▸ " + line
	}

	// Headers
	if strings.HasPrefix(line, "### ") {
		if r.colors {
			return fmt.Sprintf("\033[90m%s\033[0m", line)
		}
		return "▸ " + line[4:]
	}
	if strings.HasPrefix(line, "## ") {
		if r.colors {
			return fmt.Sprintf("\033[97m%s\033[0m", line)
		}
		return "## " + line[3:]
	}
	if strings.HasPrefix(line, "# ") {
		if r.colors {
			return fmt.Sprintf("\033[1m\033[97m%s\033[0m", line)
		}
		return "# " + line[2:]
	}

	// Unordered lists
	if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
		if r.colors {
			return fmt.Sprintf("\033[32m%s\033[0m %s", line[:2], r.renderInline(line[2:]))
		}
		return "  • " + r.renderInline(line[2:])
	}
	if strings.HasPrefix(line, "  - ") || strings.HasPrefix(line, "  * ") {
		if r.colors {
			return fmt.Sprintf("\033[32m%s\033[0m %s", line[:4], r.renderInline(line[4:]))
		}
		return "    • " + r.renderInline(line[4:])
	}

	// Ordered lists
	if orderedList.MatchString(line) {
		if r.colors {
			return fmt.Sprintf("\033[32m%s\033[0m %s", line[:2], r.renderInline(line[2:]))
		}
		return "  " + line
	}

	// Horizontal rule
	if line == "---" || line == "***" || line == "___" {
		if r.colors {
			return fmt.Sprintf("\033[90m%s\033[0m", strings.Repeat("─", 80))
		}
		return strings.Repeat("─", 80)
	}

	// Blockquote
	if strings.HasPrefix(line, "> ") {
		if r.colors {
			return fmt.Sprintf("\033[90m%s\033[0m%s", "> ", r.renderInline(line[2:]))
		}
		return "  > " + r.renderInline(line[2:])
	}

	return r.renderInline(line)
}

// renderInline applies inline formatting: bold, italic, code.
func (r *Renderer) renderInline(text string) string {
	if !r.colors {
		// Plain text mode with unicode markers
		text = boldRegex.ReplaceAllStringFunc(text, func(match string) string {
			cleaned := strings.TrimPrefix(match, "**")
			cleaned = strings.TrimSuffix(cleaned, "**")
			return "【" + cleaned + "】"
		})
		text = codeRegex.ReplaceAllStringFunc(text, func(match string) string {
			cleaned := strings.TrimPrefix(match, "`")
			cleaned = strings.TrimSuffix(cleaned, "`")
			return "「" + cleaned + "」"
		})
		return text
	}

	// Bold: **text**
	text = boldRegex.ReplaceAllStringFunc(text, func(match string) string {
		cleaned := strings.TrimPrefix(match, "**")
		cleaned = strings.TrimSuffix(cleaned, "**")
		return fmt.Sprintf("\033[1m\033[97m%s\033[0m", cleaned)
	})

	// Inline code: `code`
	text = codeRegex.ReplaceAllStringFunc(text, func(match string) string {
		cleaned := strings.TrimPrefix(match, "`")
		cleaned = strings.TrimSuffix(cleaned, "`")
		return fmt.Sprintf("\033[97m\033[47m%s\033[0m", cleaned)
	})

	return text
}

var (
	boldRegex   = regexp.MustCompile(`\*\*[^*]+\*\*`)
	codeRegex   = regexp.MustCompile("`[^`]+`")
	orderedList = regexp.MustCompile(`^\d+\.\s`)
)
