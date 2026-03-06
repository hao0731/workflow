package main

import (
	"fmt"
	"os"
)

func main() {
	_, _ = fmt.Fprintln(
		os.Stderr,
		"cmd/engine is deprecated and unsupported. Run ./workflow-api, ./orchestrator, ./scheduler, ./worker-firstparty, and ./registry instead.",
	)
	os.Exit(1)
}
