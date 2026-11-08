package markdown

import (
	"fmt"
	"regexp"
	"strings"
)

// Renderer renders markdown text for terminal display.
type Renderer struct {
	colors bool
}

// New creates a new markdown renderer.
func New(colors bool) *Renderer {
	return &Renderer{colors: colors}
}

// Render formats markdown text for terminal output.
func (r *Renderer) Render(text string) string {
	if text == "" {
		return text
	}

	// Process line by line
	lines := strings.Split(text, "\n")
	var result []string

	for i, line := range lines {
		result = append(result, r.renderLine(line, i == 0))
	}

	return strings.Join(result, "\n")
}

func (r *Renderer) renderLine(line string, isFirst bool) string {
	line = strings.TrimSpace(line)

	// Code blocks (``` ... ```)
	if strings.HasPrefix(line, "```") {
		return r.renderCodeBlock(line)
	}

	// Headers
	if strings.HasPrefix(line, "### ") {
		return r.colorize(line, "#888888") + "\n"
	}
	if strings.HasPrefix(line, "## ") {
		return r.colorize(line, "#aaaaaa") + "\n"
	}
	if strings.HasPrefix(line, "# ") {
		return r.colorize(line, "#cccccc") + "\n"
	}

	// Unordered lists
	if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
		return r.colorize(line[:2], "#4CAF50") + " " + r.renderInline(line[2:])
	}
	if strings.HasPrefix(line, "  - ") || strings.HasPrefix(line, "  * ") {
		return r.colorize(line[:4], "#4CAF50") + " " + r.renderInline(line[4:])
	}

	// Ordered lists
	if reOrderedList.MatchString(line) {
		return r.colorize(line[:2], "#4CAF50") + " " + r.renderInline(line[2:])
	}

	// Horizontal rule
	if line == "---" || line == "***" || line == "___" {
		return r.colorize(strings.Repeat("─", 80), "#555555")
	}

	// Blockquote
	if strings.HasPrefix(line, "> ") {
		return r.colorize("> ", "#888888") + r.renderInline(line[2:])
	}

	// Regular line with inline formatting
	return r.renderInline(line)
}

func (r *Renderer) renderCodeBlock(line string) string {
	// Start of code block
	if !strings.HasSuffix(line, "```") {
		return r.colorize(line, "#000000") + "\n"
	}
	// End of code block
	return r.colorize(line, "#000000") + "\n"
}

func (r *Renderer) renderInline(text string) string {
	if !r.colors {
		return text
	}

	// Bold: **text** or __text__
	text = reBold.ReplaceAllStringFunc(text, func(match string) string {
		cleaned := strings.TrimPrefix(match, "**")
		cleaned = strings.TrimPrefix(cleaned, "__")
		cleaned = strings.TrimSuffix(cleaned, "**")
		cleaned = strings.TrimSuffix(cleaned, "__")
		return r.colorize(cleaned, "#ffffff")
	})

	// Italic: *text* or _text_
	text = reItalic.ReplaceAllStringFunc(text, func(match string) string {
		cleaned := strings.TrimPrefix(match, "*")
		cleaned = strings.TrimPrefix(cleaned, "_")
		cleaned = strings.TrimSuffix(cleaned, "*")
		cleaned = strings.TrimSuffix(cleaned, "_")
		return r.colorize(cleaned, "#cccccc")
	})

	// Inline code: `code`
	text = reInlineCode.ReplaceAllStringFunc(text, func(match string) string {
		cleaned := strings.TrimPrefix(match, "`")
		cleaned = strings.TrimSuffix(cleaned, "`")
		return r.colorize(cleaned, "#f0f0f0")
	})

	// Links: [text](url)
	text = reLink.ReplaceAllStringFunc(text, func(match string) string {
		return match // Keep as-is, URL will be visible
	})

	return text
}

func (r *Renderer) colorize(text string, color string) string {
	if !r.colors {
		return text
	}
	return fmt.Sprintf("\033[%sm%s\033[0m", color, text)
}

var (
	reBold      = regexp.MustCompile(`\*\*[^*]+\*\*|__[^_]+__`)
	reItalic    = regexp.MustCompile(`(?<!\*)\*[^*]+\*(?!\*)|(?<!_)_[^_]+_(?!_)`)
	reInlineCode = regexp.MustCompile("`[^`]+`")
	reLink      = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	reOrderedList = regexp.MustCompile(`^\d+\.\s`)
)
