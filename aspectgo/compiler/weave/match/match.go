package match

import (
	"go/ast"
	"go/types"
	"regexp"

	"golang.org/x/tools/go/loader"

	log "github.com/cihub/seelog"

	"golang.org/x/exp/aspectgo/aspect"
)

// objMatchPointcut returns true if obj matches the pointcut.
// current implementation is very naive: just checks regexp for types.Func.FullName()
// TODO: support interface pointcut
func ObjMatchPointcut(prog *loader.Program, id *ast.Ident, obj types.Object, pointcut aspect.Pointcut) bool {
	fn, ok := obj.(*types.Func)
	if ok {
		return fnObjMatchPointcutByRegexp(fn, pointcut)
	}
	return false
}

func fnObjMatchPointcutByRegexp(fn *types.Func, pointcut aspect.Pointcut) bool {
	// TODO: cache compiled regexp
	re, err := regexp.Compile(string(pointcut))
	if err != nil {
		log.Warn("pointcut %s is not regexp: %s", pointcut, err)
		return false
	}
	matched := re.MatchString(fn.FullName())
	log.Debugf("matched=%t for %s (pointcut=%s)", matched, fn.FullName(), string(pointcut))
	return matched
}
