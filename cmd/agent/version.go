package main

import "fmt"

// version is overridden at build time:
//
//	go build -ldflags "-X main.version=v1.0.0" ./cmd/agent
//
// GoReleaser injects the tag version automatically.
var version = "dev"

// versionString returns the printable version string.
func versionString() string {
	return "AI Agent " + version
}

// printVersion prints the build version.
func printVersion() {
	fmt.Println(versionString())
}
