package main

import (
	"fmt"
	// "regexp"

	asp "golang.org/x/exp/aspectgo/aspect"
	"golang.org/x/exp/aspectgo/example/detreplay/worker"
)

type DetAspect struct {
}

func (a *DetAspect) Pointcut() asp.Pointcut {
	// s := regexp.QuoteMeta("(*golang.org/x/exp/aspectgo/example/detreplay/worker.W)") + ".*"
	s := ".*"
	return asp.NewCallPointcutFromRegexp(s)
}
func (a *DetAspect) Advice(ctx asp.Context) []interface{} {
	args := ctx.Args()
	recv := ctx.Receiver().(*worker.W)
	fmt.Printf("hook %d\n", recv)
	res := ctx.Call(args)
	return res
}
