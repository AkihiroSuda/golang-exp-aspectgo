package main

import (
	"golang.org/x/exp/aspectgo/compiler/cli"
	"os"
)

func main() {
	os.Exit(cli.Main(os.Args))
}
