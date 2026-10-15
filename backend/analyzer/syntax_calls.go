package analyzer

import (
	"fmt"
	"go/ast"
	"go/token"
	"sort"
	"strings"
)

var builtinFunctions = map[string]bool{
	"append": true, "cap": true, "clear": true, "close": true, "complex": true, "copy": true,
	"delete": true, "imag": true, "len": true, "make": true, "max": true, "min": true,
	"new": true, "panic": true, "print": true, "println": true, "real": true, "recover": true,
}

func (s *syntaxState) resolveCallTarget(fn *syntaxFunction, expr ast.Expr) *syntaxFunction {
	switch expr := expr.(type) {
	case *ast.Ident:
		if fn.shadowedAt(expr.Name, expr.Pos()) {
			return nil
		}
		if target := fn.pkg.functions[expr.Name]; target != nil {
			return target
		}
		var target *syntaxFunction
		for _, importPath := range fn.file.dotImports {
			if pkg := s.packages[importPath]; pkg != nil && pkg.functions[expr.Name] != nil {
				if target != nil {
					return nil
				}
				target = pkg.functions[expr.Name]
			}
		}
		return target
	case *ast.SelectorExpr:
		if alias, ok := expr.X.(*ast.Ident); ok && !fn.shadowedAt(alias.Name, alias.Pos()) {
			if importPath := fn.file.imports[alias.Name]; importPath != "" {
				if pkg := s.packages[importPath]; pkg != nil {
					return pkg.functions[expr.Sel.Name]
				}
				return nil
			}
		}
		if alias, ok := expr.X.(*ast.Ident); ok && fn.shadowedAt(alias.Name, alias.Pos()) {
			if ref, found := s.inferExpressionType(fn, expr.X); found {
				if pkg := s.packages[ref.key.pkg]; pkg != nil {
					return pkg.methods[ref.key.name][expr.Sel.Name]
				}
			}
			return nil
		}
		if ref, ok := s.resolveType(fn.file, fn, expr.X, map[typeKey]bool{}); ok {
			if pkg := s.packages[ref.key.pkg]; pkg != nil {
				if method := pkg.methods[ref.key.name][expr.Sel.Name]; method != nil {
					return method
				}
			}
			return nil
		}
		if ref, ok := s.inferExpressionType(fn, expr.X); ok {
			if pkg := s.packages[ref.key.pkg]; pkg != nil {
				return pkg.methods[ref.key.name][expr.Sel.Name]
			}
		}
	}
	return nil
}

func (s *syntaxState) scanFunction(fn *syntaxFunction) {
	if fn.node.Body == nil {
		return
	}
	start := fn.file.fset.Position(fn.node.Pos())
	var events []edgeAt
	addressed := map[*ast.CompositeLit]bool{}
	ast.Inspect(fn.node.Body, func(node ast.Node) bool {
		if literal, ok := node.(*ast.FuncLit); ok {
			line := fn.file.fset.Position(literal.Pos()).Line
			s.diagnostics = append(s.diagnostics, fmt.Sprintf("%s:%d: anonymous function body omitted", fn.id, line))
			return false
		}
		if unary, ok := node.(*ast.UnaryExpr); ok && unary.Op == token.AND {
			if literal, ok := ast.Unparen(unary.X).(*ast.CompositeLit); ok {
				if ref, ok := s.resolveType(fn.file, fn, literal.Type, map[typeKey]bool{}); ok {
					ref.pointers++
					if s.addCreation(fn, unary, ref, start.Offset, &events) {
						addressed[literal] = true
					}
				}
			}
		}
		if literal, ok := node.(*ast.CompositeLit); ok {
			if !addressed[literal] {
				if ref, ok := s.resolveType(fn.file, fn, literal.Type, map[typeKey]bool{}); ok {
					s.addCreation(fn, literal, ref, start.Offset, &events)
				}
			}
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		fun := unwrapCallExpr(call.Fun)
		if ident, ok := fun.(*ast.Ident); ok && ident.Name == "new" && !fn.shadowedAt(ident.Name, ident.Pos()) && fn.pkg.functions[ident.Name] == nil && !fn.pkg.names[ident.Name] {
			if len(call.Args) == 1 {
				if ref, ok := s.resolveType(fn.file, fn, call.Args[0], map[typeKey]bool{}); ok {
					ref.pointers++
					s.addCreation(fn, call, ref, start.Offset, &events)
				}
			}
			return true
		}
		if s.isTypeExpression(fn.file, fn, fun) {
			return true
		}
		if builtin, ok := fun.(*ast.Ident); ok && builtinFunctions[builtin.Name] && !fn.shadowedAt(builtin.Name, builtin.Pos()) && fn.pkg.functions[builtin.Name] == nil && !fn.pkg.names[builtin.Name] {
			return true
		}
		target := s.resolveCallTarget(fn, fun)
		pos := fn.file.fset.Position(call.Pos())
		if target != nil {
			events = append(events, edgeAt{edge: Edge{Caller: fn.id, Target: target.id, Line: pos.Line - start.Line + 1}, pos: pos.Offset})
			return true
		}
		s.diagnostics = append(s.diagnostics, fmt.Sprintf("%s:%d: %s", fn.id, pos.Line, s.unresolvedCallReason(fn, fun)))
		return true
	})
	sort.SliceStable(events, func(i, j int) bool { return events[i].pos < events[j].pos })
	for _, event := range events {
		s.graph.Edges = append(s.graph.Edges, event.edge)
	}
}

func (s *syntaxState) addCreation(fn *syntaxFunction, node ast.Node, ref typeReference, start int, events *[]edgeAt) bool {
	definition := s.definitions[ref.key]
	if definition == "" {
		return false
	}
	pos := fn.file.fset.Position(node.Pos())
	id := fmt.Sprintf("%s#creation:%d", fn.id, pos.Offset-start)
	name := ref.displayName
	if name == "" {
		name = ref.key.pkg + "." + ref.key.name
	}
	name += ref.args
	name = strings.Repeat("*", ref.pointers) + name
	creation := Creation{ID: id, Type: name, Definition: definition}
	s.graph.Creations = append(s.graph.Creations, creation)
	*events = append(*events, edgeAt{edge: Edge{Caller: fn.id, Target: id, Line: pos.Line - fn.file.fset.Position(fn.node.Pos()).Line + 1}, pos: pos.Offset})
	return true
}

func (s *syntaxState) unresolvedCallReason(fn *syntaxFunction, expr ast.Expr) string {
	if ident, ok := expr.(*ast.Ident); ok {
		if builtinFunctions[ident.Name] && !fn.shadowedAt(ident.Name, ident.Pos()) && fn.pkg.functions[ident.Name] == nil {
			return "unresolved builtin call omitted: " + ident.Name
		}
		return "unresolved or function-variable call omitted: " + ident.Name
	}
	if selector, ok := expr.(*ast.SelectorExpr); ok {
		if alias, ok := selector.X.(*ast.Ident); ok && !fn.shadowedAt(alias.Name, alias.Pos()) {
			if importPath := fn.file.imports[alias.Name]; importPath != "" {
				if s.isProjectPath(importPath) {
					return "unresolved project call omitted: " + importPath + "." + selector.Sel.Name
				}
				return "external call omitted: " + importPath + "." + selector.Sel.Name
			}
		}
		if ref, ok := s.inferExpressionType(fn, selector.X); ok {
			if ref.key.name == "$interface" || s.interfaces[ref.key] {
				return "interface dispatch omitted: " + ref.key.pkg + ".(" + ref.key.name + ")." + selector.Sel.Name
			}
			if !s.isProjectPath(ref.key.pkg) {
				return "external method call omitted: " + ref.key.pkg + ".(" + ref.key.name + ")." + selector.Sel.Name
			}
		}
		return "unresolved or unsupported method call omitted: " + selector.Sel.Name
	}
	return "unresolved call omitted"
}

func (s *syntaxState) isProjectPath(importPath string) bool {
	return importPath == s.modulePath || strings.HasPrefix(importPath, s.modulePath+"/")
}
