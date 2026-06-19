package main

import "gitlab.com/parendum/nexora/nexora-cli/cmd"

// version is injected at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	cmd.Version = version
	cmd.Execute()
}
