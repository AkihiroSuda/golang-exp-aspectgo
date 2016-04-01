package cli

import (
	"flag"

	log "github.com/cihub/seelog"

	"golang.org/x/exp/aspectgo/compiler"
)

func Main(args []string) int {
	var (
		debug  bool
		weave  string
		target string
	)
	f := flag.NewFlagSet(args[0], flag.ExitOnError)
	f.BoolVar(&debug, "debug", false, "enable debug print")
	f.StringVar(&weave, "w", "/tmp/wovengopath", "woven gopath")
	f.StringVar(&target, "t", "", "target package name")
	f.Parse(args[1:])
	initLog(debug)
	if target == "" {
		log.Errorf("No target package specified")
		return 1
	}
	if f.NArg() < 1 {
		log.Errorf("No aspect file specified")
		return 1
	}
	if f.NArg() >= 2 {
		log.Errorf("Too many aspect files specified: %s", f.Args())
		return 1
	}
	aspectFile := f.Args()[0]

	comp := compiler.Compiler{
		WovenGOPATH:     weave,
		Target:          target,
		AspectFilenames: []string{aspectFile},
	}
	if err := comp.Do(); err != nil {
		log.Error(err)
		return 1
	}
	return 0
}
