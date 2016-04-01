// Package compiler provides the AspectGo compiler.
package compiler

import (
	"errors"
	"fmt"
	"log"
	"os"

	"golang.org/x/exp/aspectgo/compiler/gopath"
	"golang.org/x/exp/aspectgo/compiler/parse"
	"golang.org/x/exp/aspectgo/compiler/weave"
)

// Compiler is the type for the AspectGo compiler.
type Compiler struct {
	// WovenGOPATH is the GOPATH for woven packages
	WovenGOPATH string

	// Target is the target package name
	Target string

	// AspectFilenames are aspect file names.
	// currently, only single aspect file is supported
	AspectFilenames []string
}

// Do does all the compilation phases.
func (c *Compiler) Do() error {
	log.Printf("Phase 0: Checking arguments")
	if c.WovenGOPATH == "" {
		return errors.New("WovenGOPATH not specified")
	}
	if c.Target == "" {
		return errors.New("Target not specified")
	}
	if len(c.AspectFilenames) != 1 {
		return fmt.Errorf("only single aspect file is supported at the moment: %v", c.AspectFilenames)
	}
	aspectFilename := c.AspectFilenames[0]
	oldGOPATH := os.Getenv("GOPATH")
	if oldGOPATH == "" {
		return errors.New("GOPATH not set")
	}

	log.Printf("Phase 1: Parsing the aspects")
	aspectFile, err := parse.ParseAspectFile(aspectFilename)
	if err != nil {
		return err
	}

	log.Printf("Phase 2: Weaving the aspects to the target package")
	writtenFnames, err := weave.Weave(c.WovenGOPATH, c.Target, aspectFile)
	if err != nil {
		return err
	}
	if len(writtenFnames) == 0 {
		log.Printf("Nothing to do")
		return nil
	}

	log.Printf("Phase 3: Fixing up GOPATH")
	err = gopath.FixUp(oldGOPATH, c.WovenGOPATH, writtenFnames)
	if err != nil {
		return err
	}
	return nil
}
