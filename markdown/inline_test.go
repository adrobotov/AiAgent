package markdown

import (
	"strings"
	"testing"
)

func TestRenderInlinePlain(t *testing.T) {
	r := &Renderer{colors: false}

	out := r.renderInline("some **bold** text")
	if !strings.Contains(out, "【bold】") {
		t.Fatalf("bold not rendered: %q", out)
	}

	out = r.renderInline("some `code` text")
	if !strings.Contains(out, "「code」") {
		t.Fatalf("code not rendered: %q", out)
	}
}

func TestRenderInlineColors(t *testing.T) {
	r := &Renderer{colors: true}

	out := r.renderInline("**bold**")
	if !strings.Contains(out, "\033[1m") {
		t.Fatalf("bold ANSI missing: %q", out)
	}

	out = r.renderInline("`code`")
	if !strings.Contains(out, "\033[97m") {
		t.Fatalf("code ANSI missing: %q", out)
	}
}

func TestRenderOrderedList(t *testing.T) {
	r := New(false)
	out := r.Render("1. first")
	if !strings.HasPrefix(out, "  1. ") {
		t.Fatalf("ordered list not rendered: %q", out)
	}
}

func TestRenderBlockquote(t *testing.T) {
	r := New(false)
	out := r.Render("> quoted")
	if !strings.Contains(out, ">") {
		t.Fatalf("blockquote not rendered: %q", out)
	}
}

func TestRenderSublist(t *testing.T) {
	r := New(false)
	out := r.Render("  - nested")
	if !strings.Contains(out, "•") {
		t.Fatalf("nested list not rendered: %q", out)
	}
}

func TestRenderStripANSIInput(t *testing.T) {
	r := New(false)
	out := r.Render("\x1b[31mred text\x1b[0m")
	if strings.Contains(out, "\x1b[") {
		t.Fatalf("ANSI not stripped: %q", out)
	}
	if !strings.Contains(out, "red text") {
		t.Fatalf("text lost: %q", out)
	}
}
