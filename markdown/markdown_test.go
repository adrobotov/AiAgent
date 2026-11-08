package markdown

import (
	"strings"
	"testing"
)

func TestStripANSI(t *testing.T) {
	in := "\x1b[31mred\x1b[0m and \\033[32mgreen\\033[0m and plain"
	out := stripANSI(in)
	if out != "red and green and plain" {
		t.Fatalf("stripANSI = %q", out)
	}
}

func TestRenderEmpty(t *testing.T) {
	r := New(false)
	if r.Render("") != "" {
		t.Fatal("empty input must produce empty output")
	}
}

func TestRenderHeading(t *testing.T) {
	r := New(false)
	out := r.Render("# Hello")
	if !strings.HasPrefix(out, "# ") {
		t.Fatalf("heading not preserved: %q", out)
	}
}

func TestRenderSubheading(t *testing.T) {
	r := New(false)
	out := r.Render("### Details")
	if !strings.HasPrefix(out, "▸ ") {
		t.Fatalf("subheading not rendered: %q", out)
	}
}

func TestRenderListItem(t *testing.T) {
	r := New(false)
	out := r.Render("- first item")
	if !strings.Contains(out, "• first item") {
		t.Fatalf("list item not rendered: %q", out)
	}
}

func TestRenderHorizontalRule(t *testing.T) {
	r := New(false)
	out := r.Render("---")
	if !strings.Contains(out, "─") {
		t.Fatalf("horizontal rule not rendered: %q", out)
	}
}

func TestRenderMultiline(t *testing.T) {
	r := New(false)
	in := "# Title\n\ntext\n- item"
	out := r.Render(in)
	lines := strings.Split(out, "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines, got %d: %q", len(lines), out)
	}
}

func TestRenderCodeFence(t *testing.T) {
	r := New(false)
	out := r.Render("```go")
	if !strings.HasPrefix(out, "▸ ") {
		t.Fatalf("code fence not rendered: %q", out)
	}
}
