package analyzer

import (
	"go/ast"
	"go/token"
)

func unwrapCallExpr(expr ast.Expr) ast.Expr {
	for {
		switch current := expr.(type) {
		case *ast.ParenExpr:
			expr = current.X
		case *ast.IndexExpr:
			expr = current.X
		case *ast.IndexListExpr:
			expr = current.X
		default:
			return expr
		}
	}
}

func (s *syntaxState) inferExpressionType(fn *syntaxFunction, expr ast.Expr) (typeReference, bool) {
	switch expr := expr.(type) {
	case *ast.ParenExpr:
		return s.inferExpressionType(fn, expr.X)
	case *ast.Ident:
		binding := fn.bindingAt(expr.Name, expr.Pos())
		if binding != nil {
			return binding.typ.ref, binding.typ.known && !binding.typ.ambiguous
		}
		global, exists := fn.pkg.valueTypes[expr.Name]
		return global.ref, exists && global.known && !global.ambiguous
	case *ast.SelectorExpr:
		if alias, ok := expr.X.(*ast.Ident); ok && !fn.shadowedAt(alias.Name, alias.Pos()) {
			if importPath := fn.file.imports[alias.Name]; importPath != "" {
				if pkg := s.packages[importPath]; pkg != nil {
					global, exists := pkg.valueTypes[expr.Sel.Name]
					if exists {
						return global.ref, global.known && !global.ambiguous
					}
				}
			}
		}
		if receiver, ok := s.inferExpressionType(fn, expr.X); ok {
			return s.fieldType(fn, receiver, expr.Sel.Name, map[typeKey]bool{})
		}
	case *ast.CompositeLit:
		return s.resolveType(fn.file, fn, expr.Type, map[typeKey]bool{})
	case *ast.UnaryExpr:
		ref, ok := s.inferExpressionType(fn, expr.X)
		if expr.Op == token.AND && ok {
			ref.pointers++
			return ref, true
		}
		if expr.Op == token.MUL && ok && ref.pointers > 0 {
			ref.pointers--
			return ref, true
		}
	case *ast.StarExpr:
		ref, ok := s.inferExpressionType(fn, expr.X)
		if ok && ref.pointers > 0 {
			ref.pointers--
			return ref, true
		}
	case *ast.TypeAssertExpr:
		return s.resolveType(fn.file, fn, expr.Type, map[typeKey]bool{})
	case *ast.CallExpr:
		fun := unwrapCallExpr(expr.Fun)
		if ident, ok := fun.(*ast.Ident); ok && ident.Name == "new" && !fn.shadowedAt(ident.Name, ident.Pos()) && fn.pkg.functions[ident.Name] == nil && !fn.pkg.names[ident.Name] && len(expr.Args) == 1 {
			ref, ok := s.resolveType(fn.file, fn, expr.Args[0], map[typeKey]bool{})
			if ok && ref.pointers > 0 {
				return typeReference{}, false
			}
			ref.pointers++
			return ref, ok
		}
		if s.isTypeExpression(fn.file, fn, fun) {
			return s.resolveType(fn.file, fn, fun, map[typeKey]bool{})
		}
		if target := s.resolveCallTarget(fn, fun); target != nil {
			return s.singleResultType(fn, target, expr)
		}
	}
	return typeReference{}, false
}

func (s *syntaxState) fieldType(fn *syntaxFunction, receiver typeReference, name string, seen map[typeKey]bool) (typeReference, bool) {
	if seen[receiver.key] {
		return typeReference{}, false
	}
	seen[receiver.key] = true
	var typeDecl *syntaxType
	if local := s.localTypes[receiver.key]; local != nil {
		typeDecl = local
	} else if pkg := s.packages[receiver.key.pkg]; pkg != nil {
		typeDecl = pkg.types[receiver.key.name]
	}
	if typeDecl == nil {
		return typeReference{}, false
	}
	structType, ok := typeDecl.spec.Type.(*ast.StructType)
	if !ok {
		return typeReference{}, false
	}
	arguments := map[string]typeArgument{}
	if typeDecl.spec.TypeParams != nil {
		index := 0
		for _, field := range typeDecl.spec.TypeParams.List {
			for _, param := range field.Names {
				if index < len(receiver.typeArguments) {
					arguments[param.Name] = receiver.typeArguments[index]
				}
				index++
			}
		}
	}
	for _, field := range structType.Fields.List {
		if len(field.Names) == 0 {
			if embeddedName(field.Type) == name {
				return s.resolveTypeWithArguments(typeDecl.file, nil, field.Type, map[typeKey]bool{}, arguments)
			}
			continue
		}
		for _, fieldName := range field.Names {
			if fieldName.Name == name {
				return s.resolveTypeWithArguments(typeDecl.file, nil, field.Type, map[typeKey]bool{}, arguments)
			}
		}
	}
	return typeReference{}, false
}

func embeddedName(expr ast.Expr) string {
	switch current := expr.(type) {
	case *ast.Ident:
		return current.Name
	case *ast.StarExpr:
		return embeddedName(current.X)
	case *ast.SelectorExpr:
		return current.Sel.Name
	case *ast.IndexExpr:
		return embeddedName(current.X)
	case *ast.IndexListExpr:
		return embeddedName(current.X)
	default:
		return ""
	}
}

func (s *syntaxState) singleResultType(caller, target *syntaxFunction, call *ast.CallExpr) (typeReference, bool) {
	results := target.node.Type.Results
	if results == nil {
		return typeReference{}, false
	}
	var result ast.Expr
	count := 0
	for _, field := range results.List {
		if len(field.Names) == 0 {
			count++
			result = field.Type
		} else {
			for range field.Names {
				count++
				result = field.Type
			}
		}
	}
	if count != 1 {
		return typeReference{}, false
	}
	arguments := map[string]typeArgument{}
	if target.node.Type.TypeParams != nil {
		typeArguments := callTypeArguments(call.Fun)
		index := 0
		for _, field := range target.node.Type.TypeParams.List {
			for _, name := range field.Names {
				if index < len(typeArguments) {
					arguments[name.Name] = s.makeTypeArgument(caller, typeArguments[index])
				}
				index++
			}
		}
	}
	if len(arguments) == 0 {
		if selector, ok := unwrapCallExpr(call.Fun).(*ast.SelectorExpr); ok {
			if receiver, known := s.inferExpressionType(caller, selector.X); known && receiver.args != "" {
				if receiverType := target.pkg.types[target.receiver]; receiverType != nil && receiverType.spec.TypeParams != nil {
					index := 0
					for _, field := range receiverType.spec.TypeParams.List {
						for _, name := range field.Names {
							if index < len(receiver.typeArguments) {
								arguments[name.Name] = receiver.typeArguments[index]
							}
							index++
						}
					}
				}
			}
		}
	}
	return s.resolveTypeWithArguments(target.file, target, result, map[typeKey]bool{}, arguments)
}

func callTypeArguments(expr ast.Expr) []ast.Expr {
	switch current := expr.(type) {
	case *ast.IndexExpr:
		return []ast.Expr{current.Index}
	case *ast.IndexListExpr:
		return current.Indices
	case *ast.ParenExpr:
		return callTypeArguments(current.X)
	default:
		return nil
	}
}

func (s *syntaxState) makeTypeArgument(fn *syntaxFunction, expr ast.Expr) typeArgument {
	ref, known := s.resolveType(fn.file, fn, expr, map[typeKey]bool{})
	return typeArgument{text: s.typeArgumentString(fn.file, expr, nil), ref: ref, known: known}
}
