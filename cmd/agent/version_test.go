package main

import "testing"

func TestVersionString(t *testing.T) {
	s := versionString()
	if s != "AI Agent "+version {
		t.Fatalf("versionString() = %q, want prefix 'AI Agent %s'", s, version)
	}
	if version == "" {
		t.Fatal("version must not be empty")
	}
}

func TestDispatchUnknownCommand(t *testing.T) {
	// Sanity: unknown input must not be dispatched as a command.
	for _, cmd := range commandTable() {
		if cmd.pattern.MatchString("/totally-unknown-cmd") {
			t.Fatalf("/totally-unknown-cmd matched by %s", cmd.pattern)
		}
	}
}
