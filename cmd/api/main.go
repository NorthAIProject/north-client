// Command web serves the North web application.
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := runCommand(os.Args[1:]); err != nil {
		// The logger may not exist yet when configuration fails, so this one
		// message goes straight to stderr.
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}
