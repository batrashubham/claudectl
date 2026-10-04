package main

import "github.com/batrashubham/claudectl/cmd"

// version is set at release time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	cmd.Execute(version)
}
