package gopath

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	// log "github.com/cihub/seelog"
)

func FileForNewGOPATH(s, oldGOPATH, wovenGOPATH string) (*os.File, error) {
	n := strings.Replace(s, oldGOPATH, wovenGOPATH, 1)
	d := filepath.Dir(n)
	if err := os.MkdirAll(d, 0755); err != nil {
		return nil, err
	}
	return os.Create(n)
}

type fixUpAction string

const (
	symlink fixUpAction = "symlink"
	recur               = "recur"
	skip                = "skip"
)

func exists(name string) (bool, error) {
	_, err := os.Stat(name)
	if !os.IsNotExist(err) {
		return true, nil
	} else {
		return false, err
	}
}

func isDir(name string) (bool, error) {
	resolved, err := filepath.EvalSymlinks(name)
	if err != nil {
		return false, err
	}
	fi, err := os.Lstat(resolved)
	if err != nil {
		return false, err
	}
	return fi.IsDir(), nil
}

func _fixUp(oc os.FileInfo, oldDir, wovenDir string, writtenFnames []string) (fixUpAction, string, string, error) {
	act := symlink
	ocFullName := filepath.Join(oldDir, oc.Name())
	wcFullName := filepath.Join(wovenDir, oc.Name())
	ocIsDir, _ := isDir(ocFullName)
	wcExists, _ := exists(wcFullName)
	if ocIsDir {
		for _, wf := range writtenFnames {
			if strings.HasPrefix(wf, wcFullName) {
				act = recur
				goto ret
			}
		}
	} else {
		if strings.HasSuffix(ocFullName, "_aspect.go") {
			act = skip
			goto ret
		}
		for _, wf := range writtenFnames {
			if wf == wcFullName {
				act = skip
				goto ret
			}
		}
	}
	if wcExists {
		act = skip
		goto ret
	}
ret:
	return act, ocFullName, wcFullName, nil
}

func FixUp(oldDir, wovenDir string, writtenFnames []string) error {
	ochildren, err := ioutil.ReadDir(oldDir)
	if err != nil {
		return err
	}
	for _, oc := range ochildren {
		act, ocFullName, wcFullName, err := _fixUp(oc, oldDir, wovenDir, writtenFnames)
		if err != nil {
			return err
		}
		// log.Debugf("FixUp action=%s for %s, %s",
		// 	act, ocFullName, wcFullName)
		switch act {
		case symlink:
			wgExists, _ := exists(wovenDir)
			if !wgExists {
				err = os.MkdirAll(wovenDir, 0755)
				if err != nil {
					return err
				}
			}
			err = os.Symlink(ocFullName, wcFullName)
			if err != nil {
				return err
			}
		case recur:
			err = FixUp(ocFullName, wcFullName, writtenFnames)
			if err != nil {
				return err
			}
		case skip:
			// NOP
		default:
			return fmt.Errorf("impl error: act=%s", act)
		}
	}
	return nil
}
