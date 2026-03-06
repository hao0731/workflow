package main

import (
	"fmt"
	"os"
)

func main() {
	_, _ = fmt.Fprintln(os.Stderr, "cmd/api is deprecated and unsupported. Use ./workflow-api instead.")
	os.Exit(1)
}
