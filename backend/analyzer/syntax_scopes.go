package analyzer

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"
)

func (s *syntaxState) indexFunctionScopes() {
	for _, fn := range s.functions {
		if fn.node.Type.TypeParams != nil {
			for _, field := range fn.node.Type.TypeParams.List {
				for _, name := range field.Names {
					fn.typeParams[name.Name] = true
					fn.shadows[name.Name] = true
				}
			}
		}
		if fn.node.Recv != nil {
			for _, field := range fn.node.Recv.List {
				for _, name := range field.Names {
					fn.shadows[name.Name] = true
					if ref, ok := s.resolveType(fn.file, fn, field.Type, map[typeKey]bool{}); ok {
						fn.bind(name.Name, ref, true)
					}
				}
			}
			collectReceiverTypeParams(fn, fn.node.Recv.List[0].Type)
		}
		for _, field := range fn.node.Type.Params.List {
			for _, name := range field.Names {
				fn.shadows[name.Name] = true
				if ref, ok := s.resolveType(fn.file, fn, field.Type, map[typeKey]bool{}); ok {
					fn.bind(name.Name, ref, true)
				}
			}
		}
		if fn.node.Body == nil {
			continue
		}
		ast.Inspect(fn.node.Body, func(node ast.Node) bool {
			if _, ok := node.(*ast.FuncLit); ok {
				return false
			}
			switch node := node.(type) {
			case *ast.AssignStmt:
				if node.Tok != token.DEFINE {
					return true
				}
				_, end := s.localScope(fn, node.Pos())
				start := fn.file.fset.Position(node.End()).Offset
				for i, left := range node.Lhs {
					name, ok := left.(*ast.Ident)
					if !ok || name.Name == "_" {
						continue
					}
					if i < len(node.Rhs) {
						if ref, ok := s.inferExpressionType(fn, node.Rhs[i]); ok {
							fn.bindInScope(name.Name, ref, true, start, end)
						} else {
							fn.bindInScope(name.Name, typeReference{}, false, start, end)
						}
					} else {
						fn.bindInScope(name.Name, typeReference{}, false, start, end)
					}
				}
			case *ast.ValueSpec:
				if node.Pos() < fn.node.Body.Pos() {
					return true
				}
				_, end := s.localScope(fn, node.Pos())
				start := fn.file.fset.Position(node.End()).Offset
				for i, name := range node.Names {
					if node.Type != nil {
						if ref, ok := s.resolveType(fn.file, fn, node.Type, map[typeKey]bool{}); ok {
							fn.bindInScope(name.Name, ref, true, start, end)
						} else {
							fn.bindInScope(name.Name, typeReference{}, false, start, end)
						}
					} else if i < len(node.Values) {
						if ref, ok := s.inferExpressionType(fn, node.Values[i]); ok {
							fn.bindInScope(name.Name, ref, true, start, end)
						} else {
							fn.bindInScope(name.Name, typeReference{}, false, start, end)
						}
					} else {
						fn.bindInScope(name.Name, typeReference{}, false, start, end)
					}
				}
			case *ast.RangeStmt:
				if node.Tok == token.DEFINE {
					for _, key := range []ast.Expr{node.Key, node.Value} {
						if name, ok := key.(*ast.Ident); ok && name.Name != "_" {
							start := fn.file.fset.Position(node.Body.Pos()).Offset
							end := fn.file.fset.Position(node.Body.End()).Offset
							fn.bindInScope(name.Name, typeReference{}, false, start, end)
						}
					}
				}
			case *ast.TypeSpec:
				if node.Pos() >= fn.node.Body.Pos() {
					_, end := s.localScope(fn, node.Pos())
					start := fn.file.fset.Position(node.End()).Offset
					ref := s.indexLocalType(fn, node)
					fn.bindTypeInScope(node.Name.Name, ref, start, end)
				}
			}
			return true
		})
	}
}

func (s *syntaxState) localScope(fn *syntaxFunction, pos token.Pos) (int, int) {
	start := fn.file.fset.Position(fn.node.Body.Pos()).Offset
	end := fn.file.fset.Position(fn.node.Body.End()).Offset
	span := end - start
	ast.Inspect(fn.node.Body, func(node ast.Node) bool {
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		var scope ast.Node
		switch current := node.(type) {
		case *ast.BlockStmt:
			scope = current
		case *ast.IfStmt:
			if current.Init != nil && pos >= current.Init.Pos() && pos <= current.Init.End() {
				scope = current
			}
		case *ast.ForStmt:
			if current.Init != nil && pos >= current.Init.Pos() && pos <= current.Init.End() {
				scope = current
			}
		case *ast.SwitchStmt:
			if current.Init != nil && pos >= current.Init.Pos() && pos <= current.Init.End() {
				scope = current
			}
		case *ast.TypeSwitchStmt:
			if current.Init != nil && pos >= current.Init.Pos() && pos <= current.Init.End() {
				scope = current
			}
		}
		if scope == nil || pos < scope.Pos() || pos > scope.End() {
			return true
		}
		scopeStart := fn.file.fset.Position(scope.Pos()).Offset
		scopeEnd := fn.file.fset.Position(scope.End()).Offset
		if scopeEnd-scopeStart <= span {
			start, end, span = scopeStart, scopeEnd, scopeEnd-scopeStart
		}
		return true
	})
	return start, end
}

func (s *syntaxState) indexLocalType(fn *syntaxFunction, spec *ast.TypeSpec) typeReference {
	var group *ast.GenDecl
	ast.Inspect(fn.node.Body, func(node ast.Node) bool {
		if candidate, ok := node.(*ast.GenDecl); ok && candidate.Tok == token.TYPE {
			for _, item := range candidate.Specs {
				if item == spec {
					group = candidate
					return false
				}
			}
		}
		return group == nil
	})
	if group == nil {
		return typeReference{}
	}
	if spec.Assign.IsValid() {
		if _, direct := spec.Type.(*ast.StructType); !direct {
			if ref, ok := s.resolveType(fn.file, fn, spec.Type, map[typeKey]bool{}); ok {
				return ref
			}
			return typeReference{}
		}
	}
	line := fn.file.fset.Position(spec.Pos()).Line
	key := typeKey{pkg: fn.pkg.path, name: fmt.Sprintf("%s@%s:%d", spec.Name.Name, fn.file.relative, line)}
	localType := &syntaxType{spec: spec, file: fn.file, group: group}
	s.localTypes[key] = localType
	displayName := fn.pkg.path + "." + spec.Name.Name
	underlying, hasUnderlying := s.resolveType(fn.file, fn, spec.Type, map[typeKey]bool{})
	isStruct := s.isStructType(fn.file, fn, spec.Type, map[typeKey]bool{}) || hasUnderlying && s.definitions[underlying.key] != ""
	if isStruct {
		id := key.pkg + "." + key.name
		start := fn.file.fset.Position(group.Pos()).Offset
		end := fn.file.fset.Position(group.End()).Offset
		code := "type " + string(fn.file.source[fn.file.fset.Position(spec.Pos()).Offset:fn.file.fset.Position(spec.End()).Offset])
		if !group.Lparen.IsValid() {
			code = string(fn.file.source[start:end])
		}
		doc := spec.Doc
		if doc == nil && len(group.Specs) == 1 {
			doc = group.Doc
		}
		description := "Struct " + spec.Name.Name + "."
		if doc != nil && strings.TrimSpace(doc.Text()) != "" {
			description = strings.TrimSpace(doc.Text())
		}
		s.graph.Structs = append(s.graph.Structs, StructDefinition{ID: id, Description: description, File: fn.file.relative, Code: code})
		s.definitions[key] = id
		return typeReference{key: key, displayName: displayName}
	}
	if s.isInterfaceType(fn.file, fn, spec.Type, map[typeKey]bool{}) {
		s.interfaces[key] = true
	} else if hasUnderlying && s.interfaces[underlying.key] {
		s.interfaces[key] = true
	}
	return typeReference{key: key, displayName: displayName}
}

func collectReceiverTypeParams(fn *syntaxFunction, expr ast.Expr) {
	for {
		switch current := expr.(type) {
		case *ast.ParenExpr:
			expr = current.X
		case *ast.StarExpr:
			expr = current.X
		default:
			goto collect
		}
	}
collect:
	var params []ast.Expr
	switch current := expr.(type) {
	case *ast.IndexExpr:
		params = []ast.Expr{current.Index}
	case *ast.IndexListExpr:
		params = current.Indices
	}
	for _, param := range params {
		ast.Inspect(param, func(node ast.Node) bool {
			if ident, ok := node.(*ast.Ident); ok {
				fn.typeParams[ident.Name] = true
				fn.shadows[ident.Name] = true
			}
			return true
		})
	}
}

func (fn *syntaxFunction) bind(name string, ref typeReference, known bool) {
	if fn.node.Body == nil {
		return
	}
	start := fn.file.fset.Position(fn.node.Body.Pos()).Offset
	end := fn.file.fset.Position(fn.node.Body.End()).Offset
	fn.bindInScope(name, ref, known, start, end)
}

func (fn *syntaxFunction) bindInScope(name string, ref typeReference, known bool, start, end int) {
	fn.bindings = append(fn.bindings, syntaxBinding{name: name, start: start, end: end, typ: typeBinding{ref: ref, known: known, ambiguous: !known}})
}

func (fn *syntaxFunction) bindTypeInScope(name string, ref typeReference, start, end int) {
	fn.bindings = append(fn.bindings, syntaxBinding{name: name, start: start, end: end, typ: typeBinding{ref: ref, known: true}, isType: true})
}

func (fn *syntaxFunction) bindingAt(name string, pos token.Pos) *syntaxBinding {
	offset := fn.file.fset.Position(pos).Offset
	var found *syntaxBinding
	for i := range fn.bindings {
		binding := &fn.bindings[i]
		if binding.name != name || offset < binding.start || offset > binding.end {
			continue
		}
		if found == nil || binding.start > found.start || binding.end < found.end {
			found = binding
		}
	}
	return found
}

func (fn *syntaxFunction) shadowedAt(name string, pos token.Pos) bool {
	return fn.shadows[name] || fn.bindingAt(name, pos) != nil
}
