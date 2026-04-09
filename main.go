package main

import (
	"os"

	"github.com/drizz-dev/drizz-farm/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
