package weave

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"go/types"
	"os"
	"strings"

	log "github.com/cihub/seelog"
	rewrite "github.com/tsuna/gorewrite"
	"golang.org/x/tools/go/loader"

	"golang.org/x/exp/aspectgo/aspect"
	"golang.org/x/exp/aspectgo/compiler/consts"
	"golang.org/x/exp/aspectgo/compiler/gopath"
)

func rewriteProgram(wovenGOPATH string, rw *rewriter) ([]string, error) {
	if err := rw.init(); err != nil {
		return nil, err
	}
	oldGOPATH := os.Getenv("GOPATH")
	if oldGOPATH == "" {
		return nil, fmt.Errorf("GOPATH not set")
	}
	rewrittenFnames := make([]string, 0)
	for _, pkgInfo := range rw.Program.InitialPackages() {
		for _, file := range pkgInfo.Files {
			posn := rw.Program.Fset.Position(file.Pos())
			if strings.HasSuffix(posn.Filename, "_aspect.go") {
				continue
			}
			outf, err := gopath.FileForNewGOPATH(posn.Filename,
				oldGOPATH, wovenGOPATH)
			if err != nil {
				return nil, err
			}
			defer outf.Close()
			log.Infof("Rewriting %s --> %s",
				posn.Filename, outf.Name())
			rewritten := rewrite.Rewrite(rw, file)
			outw := bufio.NewWriter(outf)
			outw.Write([]byte(consts.AutogenFileHeader))
			format.Node(outw, rw.Program.Fset, rewritten)
			for _, add := range rw.AddendumForLastASTFile() {
				outw.Write([]byte("\n"))
				format.Node(outw, rw.Program.Fset, add)
				outw.Write([]byte("\n"))
			}
			outw.Flush()
			rewrittenFnames = append(rewrittenFnames, outf.Name())
		}
	}
	return rewrittenFnames, nil
}

var gRewriterLastP = 0

// rewriter implements rewrite.Rewriter
type rewriter struct {
	Program          *loader.Program
	Matched          map[*ast.Ident]types.Object
	Aspects          map[aspect.Pointcut]*types.Named
	PointcutsByIdent map[*ast.Ident]aspect.Pointcut
	fileAddendum     []ast.Node
	proxyExprs       map[*ast.Ident]ast.Expr
}

func (r *rewriter) init() error {
	if r.Program == nil || r.Matched == nil ||
		r.Aspects == nil || r.PointcutsByIdent == nil {
		panic(log.Critical("impl error (nil args)"))
	}

	// NOTE: r.fileAddendum is initialized in Rewrite():*ast.File
	r.proxyExprs = make(map[*ast.Ident]ast.Expr)
	return nil
}

func voidIntfArrayExpr() *ast.ArrayType {
	return &ast.ArrayType{
		Elt: &ast.InterfaceType{
			Methods: &ast.FieldList{},
		}}
}

// _proxy_decl generates _ag_proxy_func decl.
func (r *rewriter) _proxy_decl(node ast.Node, matched types.Object, proxyName string) *ast.FuncDecl {
	sig := matched.Type().(*types.Signature)
	funcDecl := &ast.FuncDecl{}
	funcDecl.Name = ast.NewIdent(proxyName)
	funcDecl.Type = &ast.FuncType{}
	params, results := &ast.FieldList{}, &ast.FieldList{}
	params.List, results.List = make([]*ast.Field, 0), make([]*ast.Field, 0)
	if sig.Recv() != nil {
		// FIXME: not sure this assertion is robust
		xs, ok := node.(*ast.SelectorExpr)
		if !ok {
			panic(log.Criticalf("impl error: node=%s, recv=%s", node, sig.Recv()))
		}
		x, ok := xs.X.(*ast.Ident)
		if !ok {
			panic(log.Criticalf("impl error: node=%s, recv=%s", node, sig.Recv()))
		}
		param := &ast.Field{
			Names: []*ast.Ident{ast.NewIdent(x.Name)},
			Type: &ast.ParenExpr{
				X: ast.NewIdent(r.typeString(sig.Recv().Type(), matched.Pkg())),
			}}
		params.List = append(params.List, param)
	}
	for i := 0; i < sig.Params().Len(); i++ {
		sigParam := sig.Params().At(i)
		param := &ast.Field{}
		param.Names = []*ast.Ident{ast.NewIdent(sigParam.Name())}
		paramTypeStr := r.typeString(sigParam.Type(), matched.Pkg())
		if sig.Variadic() {
			paramTypeStr = strings.Replace(paramTypeStr, "[]", "...", 1)
		}
		param.Type = ast.NewIdent(paramTypeStr)
		params.List = append(params.List, param)
	}
	for i := 0; i < sig.Results().Len(); i++ {
		sigResult := sig.Results().At(i)
		result := &ast.Field{}
		result.Names = []*ast.Ident{ast.NewIdent(sigResult.Name())}
		result.Type = ast.NewIdent(r.typeString(sigResult.Type(), matched.Pkg()))
		results.List = append(results.List, result)
	}
	funcDecl.Type.Params, funcDecl.Type.Results = params, results
	return funcDecl
}

// _proxy_body_XArgs generates like this: `XArgs: []interface{}{"world"}`
func (r *rewriter) _proxy_body_XArgs(matched types.Object) []ast.Expr {
	sig := matched.Type().(*types.Signature)
	xArgsExprs := make([]ast.Expr, 0)
	for i := 0; i < sig.Params().Len(); i++ {
		sigParam := sig.Params().At(i)
		xArgsExprs = append(xArgsExprs, ast.NewIdent(sigParam.Name()))
	}

	return xArgsExprs
}

func (r *rewriter) _proxy_body_XFunc(node ast.Node, matched types.Object) *ast.FuncLit {
	sig := matched.Type().(*types.Signature)
	xFuncBodyStmts := make([]ast.Stmt, 0)
	xFuncBodyArgExprs := make([]ast.Expr, 0)
	for i := 0; i < sig.Params().Len(); i++ {
		sigParam := sig.Params().At(i)
		lhsName := fmt.Sprintf("_ag_arg%d", i)
		rhsType := ast.NewIdent(r.typeString(sigParam.Type(), matched.Pkg()))
		xFuncBodyArgExprs = append(xFuncBodyArgExprs,
			ast.NewIdent(lhsName))
		assignStmt := &ast.AssignStmt{
			Lhs: []ast.Expr{
				ast.NewIdent(lhsName),
			},
			Tok: token.DEFINE,
			Rhs: []ast.Expr{
				&ast.TypeAssertExpr{
					X: &ast.IndexExpr{
						X: ast.NewIdent("_ag_args"),
						Index: &ast.BasicLit{
							Kind:  token.INT,
							Value: fmt.Sprintf("%d", i),
						}},
					Type: rhsType,
				}}}
		xFuncBodyStmts = append(xFuncBodyStmts, assignStmt)
	}
	var xFuncBodyCallFuncExp ast.Expr
	switch n := node.(type) {
	case *ast.Ident:
		xFuncBodyCallFuncExp = ast.NewIdent(n.Name)
	case *ast.SelectorExpr:
		xFuncBodyCallFuncExp = &ast.SelectorExpr{
			X:   ast.NewIdent(n.X.(*ast.Ident).Name),
			Sel: ast.NewIdent(n.Sel.Name)}
	default:
		panic(log.Criticalf("impl error: %s is unexpected type: %s", n))
	}
	xFuncBodyCallLhs := make([]ast.Expr, 0)
	xFuncBodyCallLhs2 := make([]ast.Expr, 0)
	for i := 0; i < sig.Results().Len(); i++ {
		s := fmt.Sprintf("_ag_res%d", i)
		xFuncBodyCallLhs = append(xFuncBodyCallLhs,
			ast.NewIdent(s))
		xFuncBodyCallLhs2 = append(xFuncBodyCallLhs2,
			ast.NewIdent(s))
	}
	var xFuncBodyCallStmt ast.Stmt
	xFuncBodyCallExpr := &ast.CallExpr{
		Fun:  xFuncBodyCallFuncExp,
		Args: xFuncBodyArgExprs}
	if len(xFuncBodyCallLhs) > 0 {
		xFuncBodyCallStmt = &ast.AssignStmt{
			Lhs: xFuncBodyCallLhs,
			Tok: token.DEFINE,
			Rhs: []ast.Expr{xFuncBodyCallExpr}}
	} else {
		xFuncBodyCallStmt = &ast.ExprStmt{
			X: xFuncBodyCallExpr}
	}
	xFuncBodyStmts = append(xFuncBodyStmts, xFuncBodyCallStmt)
	xFuncBodyResultAssignStmt := &ast.AssignStmt{
		Lhs: []ast.Expr{ast.NewIdent("_ag_res")},
		Tok: token.DEFINE,
		Rhs: []ast.Expr{
			&ast.CompositeLit{
				Type: voidIntfArrayExpr(), Elts: xFuncBodyCallLhs2}}}
	xFuncBodyStmts = append(xFuncBodyStmts, xFuncBodyResultAssignStmt)
	xFuncBodyReturnStmt := &ast.ReturnStmt{
		Results: []ast.Expr{ast.NewIdent("_ag_res")}}
	xFuncBodyStmts = append(xFuncBodyStmts, xFuncBodyReturnStmt)

	xFuncLit := &ast.FuncLit{
		Type: &ast.FuncType{
			Params: &ast.FieldList{
				List: []*ast.Field{
					&ast.Field{
						Names: []*ast.Ident{ast.NewIdent("_ag_args")},
						Type:  voidIntfArrayExpr()}}},
			Results: &ast.FieldList{
				List: []*ast.Field{
					&ast.Field{
						Type: voidIntfArrayExpr()}}}},
		Body: &ast.BlockStmt{List: xFuncBodyStmts}}

	return xFuncLit
}

func (r *rewriter) _proxy_body_XReceiver(node ast.Node, matched types.Object) ast.Expr {
	sig := matched.Type().(*types.Signature)
	recv := sig.Recv()
	if recv != nil {
		// FIXME: not sure this assertion is robust
		xs, ok := node.(*ast.SelectorExpr)
		if !ok {
			panic(log.Criticalf("impl error: node=%s, recv=%s", node, sig.Recv()))
		}
		x, ok := xs.X.(*ast.Ident)
		if !ok {
			panic(log.Criticalf("impl error: node=%s, recv=%s", node, sig.Recv()))
		}
		return ast.NewIdent(x.Name)
	} else {
		return ast.NewIdent("nil")
	}
}

func (r *rewriter) _proxy_body_callExpr(node ast.Node, matched types.Object, asp *types.Named) *ast.CallExpr {
	callExpr := &ast.CallExpr{}
	adviceExpr := &ast.SelectorExpr{
		X: &ast.ParenExpr{
			X: &ast.UnaryExpr{
				Op: token.AND,
				X: &ast.CompositeLit{
					Type: &ast.SelectorExpr{
						X:   ast.NewIdent("agaspect"),
						Sel: ast.NewIdent(asp.Obj().Name()),
					}}}},
		Sel: &ast.Ident{
			Name: "Advice",
		}}

	ctxExpr := &ast.UnaryExpr{
		Op: token.AND,
		X: &ast.CompositeLit{
			Type: &ast.SelectorExpr{
				X:   ast.NewIdent("aspectrt"),
				Sel: ast.NewIdent("ContextImpl"),
			},
			Elts: []ast.Expr{
				&ast.KeyValueExpr{
					Key: ast.NewIdent("XArgs"),
					Value: &ast.CompositeLit{
						Type: voidIntfArrayExpr(),
						Elts: r._proxy_body_XArgs(matched),
					}},
				&ast.KeyValueExpr{
					Key:   ast.NewIdent("XFunc"),
					Value: r._proxy_body_XFunc(node, matched),
				},
				&ast.KeyValueExpr{
					Key:   ast.NewIdent("XReceiver"),
					Value: r._proxy_body_XReceiver(node, matched),
				}}}}

	callExpr.Fun = adviceExpr
	callExpr.Args = []ast.Expr{ctxExpr}
	return callExpr
}

// _proxy_body generates _ag_proxy_func body like this:
//
// _ag_res := (&dummyAspect{}).Advice(
// 	&ContextImpl{
// 		XArgs: []interface{}{"world"},
// 		XFunc: func(_ag_args []interface{}) []interface{} {
// 			_ag_arg0 := _ag_args[0].(string)
// 			sayHello(_ag_arg0)
// 			_ag_res := []interface{}{}
// 			return _ag_res
// 		}})
// _ = _ag_res
// return
func (r *rewriter) _proxy_body(node ast.Node, matched types.Object, asp *types.Named) *ast.BlockStmt {
	stmts := make([]ast.Stmt, 0)
	stmts = append(stmts,
		&ast.AssignStmt{
			Lhs: []ast.Expr{ast.NewIdent("_ag_res")},
			Tok: token.DEFINE,
			Rhs: []ast.Expr{r._proxy_body_callExpr(node, matched, asp)}})

	sig := matched.Type().(*types.Signature)
	resAssignStmts := make([]ast.Stmt, 0)
	resExprs := make([]ast.Expr, 0)
	for i := 0; i < sig.Results().Len(); i++ {
		sigResult := sig.Results().At(i)
		stmt := &ast.AssignStmt{
			Lhs: []ast.Expr{ast.NewIdent(fmt.Sprintf("_ag_res%d", i)),
				ast.NewIdent("_")},
			Tok: token.DEFINE,
			Rhs: []ast.Expr{
				&ast.TypeAssertExpr{
					X: &ast.IndexExpr{
						X: ast.NewIdent("_ag_res"),
						Index: &ast.BasicLit{
							Kind:  token.INT,
							Value: fmt.Sprintf("%d", i),
						}},
					Type: ast.NewIdent(r.typeString(sigResult.Type(), matched.Pkg()))}}}
		resAssignStmts = append(resAssignStmts, stmt)
		resExprs = append(resExprs, ast.NewIdent(fmt.Sprintf("_ag_res%d", i)))
	}

	stmts = append(stmts,
		&ast.AssignStmt{
			Lhs: []ast.Expr{ast.NewIdent("_")},
			Tok: token.ASSIGN,
			Rhs: []ast.Expr{ast.NewIdent("_ag_res")}})
	stmts = append(stmts, resAssignStmts...)
	stmts = append(stmts, &ast.ReturnStmt{Results: resExprs})
	res := &ast.BlockStmt{List: stmts}
	return res
}

func (r *rewriter) _proxy(node ast.Node, matched types.Object, proxyName string, asp *types.Named) *ast.FuncDecl {
	funcDecl := r._proxy_decl(node, matched, proxyName)
	funcDecl.Body = r._proxy_body(node, matched, asp)
	return funcDecl
}

func (r *rewriter) _pgen_decl(matched types.Object, pdecl *ast.FuncDecl, pgenName string) *ast.FuncDecl {
	sig := matched.Type().(*types.Signature)
	receiver := sig.Recv()
	funcDecl := &ast.FuncDecl{}
	funcDecl.Name = ast.NewIdent(pgenName)
	funcDecl.Type = &ast.FuncType{}
	params, results := &ast.FieldList{}, &ast.FieldList{}
	params.List, results.List = make([]*ast.Field, 0), make([]*ast.Field, 0)

	if receiver != nil {
		pdeclRecv := pdecl.Type.Params.List[0]
		name := pdeclRecv.Names[0].Name
		typ := r.typeString(receiver.Type(), matched.Pkg())
		param := &ast.Field{
			Names: []*ast.Ident{ast.NewIdent(name)},
			Type: &ast.ParenExpr{
				X: ast.NewIdent(typ)}}
		params.List = append(params.List, param)
	}

	pdParamsL, pdResultsL := make([]*ast.Field, 0), make([]*ast.Field, 0)
	pdParamScanBegin := 0
	if receiver != nil {
		pdParamScanBegin = 1
	}
	for i := pdParamScanBegin; i < len(pdecl.Type.Params.List); i++ {
		pdParam := &ast.Field{
			Type: pdecl.Type.Params.List[i].Type,
		}
		pdParamsL = append(pdParamsL, pdParam)
	}
	for i := 0; i < len(pdecl.Type.Results.List); i++ {
		pdResult := &ast.Field{
			Type: pdecl.Type.Results.List[i].Type,
		}
		pdResultsL = append(pdResultsL, pdResult)
	}
	result := &ast.Field{
		Type: &ast.FuncType{
			Params: &ast.FieldList{
				List: pdParamsL,
			},
			Results: &ast.FieldList{
				List: pdResultsL,
			}}}
	results.List = append(results.List, result)
	funcDecl.Type.Params, funcDecl.Type.Results = params, results
	return funcDecl
}

func (r *rewriter) _pgen_body(matched types.Object, pdecl *ast.FuncDecl) *ast.BlockStmt {
	sig := matched.Type().(*types.Signature)
	receiver := sig.Recv()

	funcLit := &ast.FuncLit{}
	funcLit.Type = &ast.FuncType{}
	params, results := &ast.FieldList{}, &ast.FieldList{}
	pdParamsL, pdResultsL := make([]*ast.Field, 0), make([]*ast.Field, 0)
	pdParamScanBegin := 0
	if receiver != nil {
		pdParamScanBegin = 1
	}
	for i := pdParamScanBegin; i < len(pdecl.Type.Params.List); i++ {
		pdParam := &ast.Field{
			Names: pdecl.Type.Params.List[i].Names,
			Type:  pdecl.Type.Params.List[i].Type,
		}
		pdParamsL = append(pdParamsL, pdParam)
	}
	for i := 0; i < len(pdecl.Type.Results.List); i++ {
		pdResult := &ast.Field{
			Names: pdecl.Type.Results.List[i].Names,
			Type:  pdecl.Type.Results.List[i].Type,
		}
		pdResultsL = append(pdResultsL, pdResult)
	}
	params.List, results.List = pdParamsL, pdResultsL
	funcLit.Type.Params, funcLit.Type.Results = params, results

	funcLitArgs := make([]ast.Expr, 0)
	for i := 0; i < len(pdecl.Type.Params.List); i++ {
		for j := 0; j < len(pdecl.Type.Params.List[i].Names); j++ {
			funcLitArgs = append(funcLitArgs,
				ast.NewIdent(pdecl.Type.Params.List[i].Names[j].Name))
		}
	}

	funcLitBodyExpr := &ast.CallExpr{
		Fun:  ast.NewIdent(pdecl.Name.Name),
		Args: funcLitArgs,
	}
	var funcLitBodyStmt ast.Stmt
	if len(pdecl.Type.Results.List) == 0 {
		funcLitBodyStmt = &ast.ExprStmt{
			X: funcLitBodyExpr,
		}
	} else {
		funcLitBodyStmt = &ast.ReturnStmt{
			Results: []ast.Expr{funcLitBodyExpr},
		}
	}
	funcLit.Body = &ast.BlockStmt{List: []ast.Stmt{funcLitBodyStmt}}

	return &ast.BlockStmt{
		List: []ast.Stmt{
			&ast.ReturnStmt{
				Results: []ast.Expr{funcLit},
			},
		}}
}

// _pgen generates the pgen function.
//
// pgen is like this:
//
// var f func(int)
// f := (_ag_pgen_ag_proxy_0(i)) // orig: f := i.Foo
// f(42)
//
// func _ag_pgen_ag_proxy_0(i I) func(int) {
// 	return func(x int){_ag_proxy_0(i, x)}
// }
// ​
// func _ag_proxy_0(i I, x int) {
//   ..
// }
func (r *rewriter) _pgen(matched types.Object, pdecl *ast.FuncDecl, pgenName string) *ast.FuncDecl {
	funcDecl := r._pgen_decl(matched, pdecl, pgenName)
	funcDecl.Body = r._pgen_body(matched, pdecl)
	return funcDecl
}

func (r *rewriter) _proxy_fix_up(node ast.Node, matched types.Object, pgenName string) ast.Expr {
	sig := matched.Type().(*types.Signature)
	args := make([]ast.Expr, 0)
	if sig.Recv() != nil {
		// FIXME: not sure this assertion is robust
		// FIXME: not sure this assertion is robust
		xs, ok := node.(*ast.SelectorExpr)
		if !ok {
			panic(log.Criticalf("impl error: node=%s, recv=%s", node, sig.Recv()))
		}
		x, ok := xs.X.(*ast.Ident)
		if !ok {
			panic(log.Criticalf("impl error: node=%s, recv=%s", node, sig.Recv()))
		}
		args = append(args, ast.NewIdent(x.Name))
	}
	callExpr := &ast.CallExpr{
		Fun:  ast.NewIdent(pgenName),
		Args: args,
	}
	parenExpr := &ast.ParenExpr{
		X: callExpr,
	}
	return parenExpr
}

func (r *rewriter) proxy(node ast.Node, pointcut aspect.Pointcut) ast.Expr {
	var id *ast.Ident
	switch n := node.(type) {
	case *ast.Ident:
		id = n
	case *ast.SelectorExpr:
		id = n.Sel
	default:
		panic(log.Criticalf("impl error: %s is unexpected type: %s", n))
	}
	// alreadyGen, ok := r.proxyExprs[id]
	// if ok {
	// 	return alreadyGen
	// }

	matched, ok := r.Matched[id]
	if !ok {
		panic(log.Criticalf("impl error: obj not found for id %s", id))
	}
	asp, ok := r.Aspects[pointcut]
	if !ok {
		panic(log.Criticalf("impl error: asp %s not found for pointcut %s", asp, pointcut))
	}

	proxyName := fmt.Sprintf("_ag_proxy_%d", gRewriterLastP)
	pgenName := fmt.Sprintf("_ag_pgen%s", proxyName)
	gRewriterLastP += 1

	proxyAst := r._proxy(node, matched, proxyName, asp)
	r.fileAddendum = append(r.fileAddendum, proxyAst)

	pgenAst := r._pgen(matched, proxyAst, pgenName)
	r.fileAddendum = append(r.fileAddendum, pgenAst)

	expr := r._proxy_fix_up(node, matched, pgenName)
	r.proxyExprs[id] = expr
	return expr
}

func (r *rewriter) Rewrite(node ast.Node) (ast.Node, rewrite.Rewriter) {
	switch n := node.(type) {
	case *ast.File:
		r.fileAddendum = make([]ast.Node, 0)
		newImports := []*ast.ImportSpec{
			&ast.ImportSpec{
				Name: ast.NewIdent("aspectrt"),
				Path: &ast.BasicLit{
					Kind:  token.STRING,
					Value: "\"golang.org/x/exp/aspectgo/aspect/rt\"",
				}},
			&ast.ImportSpec{
				Path: &ast.BasicLit{
					Kind:  token.STRING,
					Value: "\"agaspect\"",
				}},
		}
		newFile := &ast.File{}
		newFile.Name = ast.NewIdent(n.Name.Name)
		newFile.Decls = append([]ast.Decl{
			&ast.GenDecl{
				Tok:   token.IMPORT,
				Specs: []ast.Spec{newImports[0]}},
			&ast.GenDecl{
				Tok:   token.IMPORT,
				Specs: []ast.Spec{newImports[1]}},
		}, n.Decls...)
		newFile.Scope = n.Scope
		newFile.Imports = append(newImports, n.Imports...)
		newFile.Unresolved = n.Unresolved
		return newFile, r
	case *ast.Ident:
		pointcut, ok := r.PointcutsByIdent[n]
		if !ok {
			goto nop
		}
		newExpr := r.proxy(n, pointcut)
		return newExpr, nil
	case *ast.SelectorExpr:
		pointcut, ok := r.PointcutsByIdent[n.Sel]
		if !ok {
			goto nop
		}
		newExpr := r.proxy(n, pointcut)
		return newExpr, nil
	}
nop:
	return node, r
}

func (r *rewriter) AddendumForLastASTFile() []ast.Node {
	return r.fileAddendum
}

func (r *rewriter) typeString(typ types.Type, pkg *types.Package) string {
	return types.TypeString(typ,
		types.RelativeTo(pkg))
}