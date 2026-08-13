package main

import (
	"fmt"
	"os"

	"github.com/bnkrr/rses/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
