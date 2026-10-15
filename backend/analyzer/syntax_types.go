package analyzer

import (
	"go/ast"
	"go/format"
	"strings"
)

func (s *syntaxState) resolveType(file *syntaxFile, fn *syntaxFunction, expr ast.Expr, seen map[typeKey]bool) (typeReference, bool) {
	return s.resolveTypeWithArguments(file, fn, expr, seen, nil)
}

func (s *syntaxState) resolveTypeWithArguments(file *syntaxFile, fn *syntaxFunction, expr ast.Expr, seen map[typeKey]bool, arguments map[string]typeArgument) (typeReference, bool) {
	if expr == nil {
		return typeReference{}, false
	}
	switch expr := expr.(type) {
	case *ast.ParenExpr:
		return s.resolveTypeWithArguments(file, fn, expr.X, seen, arguments)
	case *ast.StarExpr:
		ref, ok := s.resolveTypeWithArguments(file, fn, expr.X, seen, arguments)
		ref.pointers++
		return ref, ok
	case *ast.IndexExpr:
		ref, ok := s.resolveTypeWithArguments(file, fn, expr.X, seen, arguments)
		if ok {
			ref.args = "[" + s.typeArgumentString(file, expr.Index, arguments) + "]"
			ref.typeArguments = []typeArgument{s.makeTypeArgumentWithArguments(file, fn, expr.Index, arguments)}
		}
		return ref, ok
	case *ast.IndexListExpr:
		ref, ok := s.resolveTypeWithArguments(file, fn, expr.X, seen, arguments)
		if ok {
			args := make([]string, 0, len(expr.Indices))
			typeArguments := make([]typeArgument, 0, len(expr.Indices))
			for _, arg := range expr.Indices {
				args = append(args, s.typeArgumentString(file, arg, arguments))
				typeArguments = append(typeArguments, s.makeTypeArgumentWithArguments(file, fn, arg, arguments))
			}
			ref.args = "[" + strings.Join(args, ", ") + "]"
			ref.typeArguments = typeArguments
		}
		return ref, ok
	case *ast.Ident:
		if fn != nil {
			if binding := fn.bindingAt(expr.Name, expr.Pos()); binding != nil {
				if !binding.isType {
					return typeReference{}, false
				}
				return binding.typ.ref, binding.typ.known && !binding.typ.ambiguous
			}
		}
		if argument, ok := arguments[expr.Name]; ok {
			return argument.ref, argument.known
		}
		if fn != nil && fn.typeParams[expr.Name] {
			return typeReference{}, false
		}
		localType := file.pkg.types[expr.Name]
		if localType == nil {
			return typeReference{}, false
		}
		key := typeKey{pkg: file.pkg.path, name: expr.Name}
		if seen[key] {
			return typeReference{}, false
		}
		if localType.spec.Assign.IsValid() && s.definitions[key] == "" {
			seen[key] = true
			return s.resolveTypeWithArguments(localType.file, nil, localType.spec.Type, seen, arguments)
		}
		return typeReference{key: key, displayName: key.pkg + "." + key.name}, true
	case *ast.SelectorExpr:
		alias, ok := expr.X.(*ast.Ident)
		if !ok {
			return typeReference{}, false
		}
		if fn != nil && fn.shadowedAt(alias.Name, alias.Pos()) {
			return typeReference{}, false
		}
		importPath := file.imports[alias.Name]
		if importPath == "" {
			return typeReference{}, false
		}
		key := typeKey{pkg: importPath, name: expr.Sel.Name}
		pkg := s.packages[importPath]
		if pkg != nil {
			localType := pkg.types[expr.Sel.Name]
			if localType == nil {
				return typeReference{}, false
			}
			if seen[key] {
				return typeReference{}, false
			}
			if localType.spec.Assign.IsValid() && s.definitions[key] == "" {
				seen[key] = true
				return s.resolveTypeWithArguments(localType.file, nil, localType.spec.Type, seen, arguments)
			}
		}
		return typeReference{key: key, displayName: key.pkg + "." + key.name}, true
	default:
		return typeReference{}, false
	}
}

func (s *syntaxState) typeArgumentString(file *syntaxFile, expr ast.Expr, arguments map[string]typeArgument) string {
	switch expr := expr.(type) {
	case *ast.Ident:
		if argument, exists := arguments[expr.Name]; exists {
			return argument.text
		}
	case *ast.ParenExpr:
		return "(" + s.typeArgumentString(file, expr.X, arguments) + ")"
	case *ast.StarExpr:
		return "*" + s.typeArgumentString(file, expr.X, arguments)
	case *ast.IndexExpr:
		return s.typeArgumentString(file, expr.X, arguments) + "[" + s.typeArgumentString(file, expr.Index, arguments) + "]"
	case *ast.IndexListExpr:
		args := make([]string, 0, len(expr.Indices))
		for _, arg := range expr.Indices {
			args = append(args, s.typeArgumentString(file, arg, arguments))
		}
		return s.typeArgumentString(file, expr.X, arguments) + "[" + strings.Join(args, ", ") + "]"
	case *ast.ArrayType:
		length := ""
		if expr.Len != nil {
			length = s.exprString(file, expr.Len)
		}
		return "[" + length + "]" + s.typeArgumentString(file, expr.Elt, arguments)
	case *ast.MapType:
		return "map[" + s.typeArgumentString(file, expr.Key, arguments) + "]" + s.typeArgumentString(file, expr.Value, arguments)
	}
	return s.exprString(file, expr)
}

func (s *syntaxState) makeTypeArgumentWithArguments(file *syntaxFile, fn *syntaxFunction, expr ast.Expr, arguments map[string]typeArgument) typeArgument {
	if ident, ok := expr.(*ast.Ident); ok {
		if argument, exists := arguments[ident.Name]; exists {
			return argument
		}
	}
	ref, known := s.resolveTypeWithArguments(file, fn, expr, map[typeKey]bool{}, arguments)
	return typeArgument{text: s.typeArgumentString(file, expr, arguments), ref: ref, known: known}
}

func (s *syntaxState) exprString(file *syntaxFile, expr ast.Expr) string {
	var output strings.Builder
	if err := format.Node(&output, file.fset, expr); err != nil {
		return ""
	}
	return output.String()
}

func (s *syntaxState) isTypeExpression(file *syntaxFile, fn *syntaxFunction, expr ast.Expr) bool {
	switch expr := expr.(type) {
	case *ast.ParenExpr:
		return s.isTypeExpression(file, fn, expr.X)
	case *ast.StarExpr, *ast.ArrayType, *ast.MapType, *ast.ChanType, *ast.StructType, *ast.InterfaceType, *ast.FuncType:
		return true
	case *ast.IndexExpr:
		return s.isTypeExpression(file, fn, expr.X)
	case *ast.IndexListExpr:
		return s.isTypeExpression(file, fn, expr.X)
	case *ast.Ident:
		if binding := fn.bindingAt(expr.Name, expr.Pos()); binding != nil {
			return binding.isType
		}
		return builtinTypes[expr.Name] || fn.typeParams[expr.Name] || file.pkg.types[expr.Name] != nil
	case *ast.SelectorExpr:
		alias, ok := expr.X.(*ast.Ident)
		if !ok {
			return false
		}
		if fn.shadowedAt(alias.Name, alias.Pos()) {
			return false
		}
		pkg := s.packages[file.imports[alias.Name]]
		return pkg != nil && pkg.types[expr.Sel.Name] != nil
	default:
		return false
	}
}

var builtinTypes = map[string]bool{
	"any": true, "bool": true, "byte": true, "comparable": true, "complex64": true,
	"complex128": true, "error": true, "float32": true, "float64": true, "int": true,
	"int8": true, "int16": true, "int32": true, "int64": true, "rune": true, "string": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true, "uintptr": true,
}
