package main

import "github.com/batrashubham/claudectl/cmd"

// version is set by goreleaser via -ldflags "-X main.version=...".
var version = ""

func main() {
	cmd.SetVersion(version)
	cmd.Execute()
}
