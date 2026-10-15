package analyzer

import (
	"fmt"
	"go/ast"
	"go/token"
	"path"
	"sort"
	"strconv"
	"strings"
)

func (s *syntaxState) indexImports() {
	for _, pkg := range s.packages {
		for _, file := range pkg.files {
			for _, spec := range file.ast.Imports {
				importPath, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					continue
				}
				alias := path.Base(importPath)
				if imported := s.packages[importPath]; imported != nil {
					alias = imported.name
				}
				if spec.Name != nil {
					alias = spec.Name.Name
				}
				if alias == "." {
					file.dotImports = append(file.dotImports, importPath)
				} else if alias != "_" {
					file.imports[alias] = importPath
				}
			}
		}
	}
}

func (s *syntaxState) indexTypes() {
	for _, pkg := range s.packages {
		for _, file := range pkg.files {
			for _, decl := range file.ast.Decls {
				switch decl := decl.(type) {
				case *ast.FuncDecl:
					if decl.Recv == nil {
						pkg.names[decl.Name.Name] = true
					}
				case *ast.GenDecl:
					for _, spec := range decl.Specs {
						switch spec := spec.(type) {
						case *ast.TypeSpec:
							pkg.names[spec.Name.Name] = true
						case *ast.ValueSpec:
							for _, name := range spec.Names {
								pkg.names[name.Name] = true
							}
						}
					}
				}
			}
		}
	}
	for _, pkg := range s.packages {
		for _, file := range pkg.files {
			for _, decl := range file.ast.Decls {
				group, ok := decl.(*ast.GenDecl)
				if !ok || group.Tok != token.TYPE {
					continue
				}
				for _, spec := range group.Specs {
					typeSpec := spec.(*ast.TypeSpec)
					pkg.types[typeSpec.Name.Name] = &syntaxType{spec: typeSpec, file: file, group: group}
				}
			}
		}
	}
	for _, pkg := range s.packages {
		for _, file := range pkg.files {
			for _, decl := range file.ast.Decls {
				group, ok := decl.(*ast.GenDecl)
				if !ok || group.Tok != token.VAR && group.Tok != token.CONST {
					continue
				}
				for _, item := range group.Specs {
					spec := item.(*ast.ValueSpec)
					for i, name := range spec.Names {
						var ref typeReference
						var known bool
						if spec.Type != nil {
							ref, known = s.resolveType(file, nil, spec.Type, map[typeKey]bool{})
						} else if i < len(spec.Values) {
							ref, known = s.inferGlobalExpressionType(file, spec.Values[i])
						}
						pkg.valueTypes[name.Name] = typeBinding{ref: ref, known: known, ambiguous: !known}
					}
				}
			}
		}
	}
	for _, pkg := range s.packages {
		for name, localType := range pkg.types {
			key := typeKey{pkg: pkg.path, name: name}
			if s.isInterfaceType(localType.file, nil, localType.spec.Type, map[typeKey]bool{}) {
				s.interfaces[key] = true
			}
			if !s.isStructType(localType.file, nil, localType.spec.Type, map[typeKey]bool{}) {
				continue
			}
			if localType.spec.Assign.IsValid() {
				if _, direct := localType.spec.Type.(*ast.StructType); !direct {
					continue
				}
			}
			id := pkg.path + "." + name
			start := localType.file.pkgFileOffset(localType.group.Pos())
			end := localType.file.pkgFileOffset(localType.group.End())
			code := "type " + string(localType.file.source[localType.file.pkgFileOffset(localType.spec.Pos()):localType.file.pkgFileOffset(localType.spec.End())])
			if !localType.group.Lparen.IsValid() {
				code = string(localType.file.source[start:end])
			}
			doc := localType.spec.Doc
			if doc == nil && len(localType.group.Specs) == 1 {
				doc = localType.group.Doc
			}
			description := "Struct " + name + "."
			if doc != nil && strings.TrimSpace(doc.Text()) != "" {
				description = strings.TrimSpace(doc.Text())
			}
			s.graph.Structs = append(s.graph.Structs, StructDefinition{ID: id, Description: description, File: localType.file.relative, Code: code})
			s.definitions[key] = id
		}
	}
}

func (s *syntaxState) inferGlobalExpressionType(file *syntaxFile, expr ast.Expr) (typeReference, bool) {
	switch expr := expr.(type) {
	case *ast.CompositeLit:
		return s.resolveType(file, nil, expr.Type, map[typeKey]bool{})
	case *ast.UnaryExpr:
		if expr.Op == token.AND {
			if literal, ok := ast.Unparen(expr.X).(*ast.CompositeLit); ok {
				if ref, ok := s.resolveType(file, nil, literal.Type, map[typeKey]bool{}); ok {
					ref.pointers++
					return ref, true
				}
			}
		}
	case *ast.CallExpr:
		if ident, ok := unwrapCallExpr(expr.Fun).(*ast.Ident); ok && ident.Name == "new" && len(expr.Args) == 1 {
			ref, known := s.resolveType(file, nil, expr.Args[0], map[typeKey]bool{})
			if known {
				ref.pointers++
			}
			return ref, known
		}
	}
	return typeReference{}, false
}

func (f *syntaxFile) pkgFileOffset(pos token.Pos) int {
	return f.fset.Position(pos).Offset
}

func (s *syntaxState) isStructType(file *syntaxFile, fn *syntaxFunction, expr ast.Expr, seen map[typeKey]bool) bool {
	switch expr := expr.(type) {
	case *ast.StructType:
		return true
	case *ast.ParenExpr:
		return s.isStructType(file, fn, expr.X, seen)
	case *ast.IndexExpr:
		return s.isStructType(file, fn, expr.X, seen)
	case *ast.IndexListExpr:
		return s.isStructType(file, fn, expr.X, seen)
	case *ast.Ident:
		if fn != nil && fn.typeParams[expr.Name] {
			return false
		}
		return s.isNamedStruct(file.pkg, expr.Name, seen)
	case *ast.SelectorExpr:
		alias, ok := expr.X.(*ast.Ident)
		if !ok {
			return false
		}
		importPath := file.imports[alias.Name]
		pkg := s.packages[importPath]
		return pkg != nil && s.isNamedStruct(pkg, expr.Sel.Name, seen)
	default:
		return false
	}
}

func (s *syntaxState) isNamedStruct(pkg *syntaxPackage, name string, seen map[typeKey]bool) bool {
	localType := pkg.types[name]
	if localType == nil {
		return false
	}
	key := typeKey{pkg: pkg.path, name: name}
	if seen[key] {
		return false
	}
	seen[key] = true
	return s.isStructType(localType.file, nil, localType.spec.Type, seen)
}

func (s *syntaxState) isInterfaceType(file *syntaxFile, fn *syntaxFunction, expr ast.Expr, seen map[typeKey]bool) bool {
	switch expr := expr.(type) {
	case *ast.InterfaceType:
		return true
	case *ast.ParenExpr:
		return s.isInterfaceType(file, fn, expr.X, seen)
	case *ast.IndexExpr:
		return s.isInterfaceType(file, fn, expr.X, seen)
	case *ast.IndexListExpr:
		return s.isInterfaceType(file, fn, expr.X, seen)
	case *ast.Ident:
		if fn != nil && fn.typeParams[expr.Name] {
			return false
		}
		localType := file.pkg.types[expr.Name]
		if localType == nil {
			return false
		}
		key := typeKey{pkg: file.pkg.path, name: expr.Name}
		if seen[key] {
			return false
		}
		seen[key] = true
		return s.isInterfaceType(localType.file, nil, localType.spec.Type, seen)
	case *ast.SelectorExpr:
		alias, ok := expr.X.(*ast.Ident)
		if !ok {
			return false
		}
		pkg := s.packages[file.imports[alias.Name]]
		if pkg == nil {
			return false
		}
		localType := pkg.types[expr.Sel.Name]
		if localType == nil {
			return false
		}
		key := typeKey{pkg: pkg.path, name: expr.Sel.Name}
		if seen[key] {
			return false
		}
		seen[key] = true
		return s.isInterfaceType(localType.file, nil, localType.spec.Type, seen)
	default:
		return false
	}
}

func (s *syntaxState) indexFunctions() {
	for _, pkg := range s.packages {
		for _, file := range pkg.files {
			for _, decl := range file.ast.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				item := &syntaxFunction{node: fn, file: file, pkg: pkg, shadows: map[string]bool{}, typeParams: map[string]bool{}}
				if fn.Recv != nil && len(fn.Recv.List) > 0 {
					item.receiver, item.receiverPtr = receiverTypeName(fn.Recv.List[0].Type)
				}
				if item.receiver == "" {
					item.id = pkg.path + "." + fn.Name.Name
				} else {
					receiver := item.receiver
					if item.receiverPtr {
						receiver = "*" + receiver
					}
					item.id = pkg.path + ".(" + receiver + ")." + fn.Name.Name
				}
				if fn.Name.Name == "init" && fn.Recv == nil {
					line := file.fset.Position(fn.Pos()).Line
					item.id += fmt.Sprintf("@%s:%d", file.relative, line)
				}
				if fn.Recv == nil {
					pkg.functions[fn.Name.Name] = item
				} else {
					if pkg.methods[item.receiver] == nil {
						pkg.methods[item.receiver] = map[string]*syntaxFunction{}
					}
					pkg.methods[item.receiver][fn.Name.Name] = item
				}
				s.functions = append(s.functions, item)
				start := file.fset.Position(fn.Pos()).Offset
				end := file.fset.Position(fn.End()).Offset
				code := string(file.source[start:end])
				s.graph.Functions = append(s.graph.Functions, Function{ID: item.id, Description: functionDescription(fn), File: file.relative, Code: code})
				if pkg.name == "main" && fn.Name.Name == "main" && fn.Recv == nil {
					s.graph.Entrypoints = append(s.graph.Entrypoints, item.id)
				}
			}
		}
	}
	sort.Slice(s.functions, func(i, j int) bool { return s.functions[i].id < s.functions[j].id })
}

func receiverTypeName(expr ast.Expr) (string, bool) {
	pointer := false
	for {
		switch current := expr.(type) {
		case *ast.ParenExpr:
			expr = current.X
		case *ast.StarExpr:
			pointer = true
			expr = current.X
		case *ast.IndexExpr:
			expr = current.X
		case *ast.IndexListExpr:
			expr = current.X
		default:
			name, _ := expr.(*ast.Ident)
			if name == nil {
				return "", pointer
			}
			return name.Name, pointer
		}
	}
}

func functionDescription(fn *ast.FuncDecl) string {
	if description := strings.TrimSpace(fn.Doc.Text()); description != "" {
		return description
	}
	if fn.Recv != nil {
		return "Method " + fn.Name.Name + "."
	}
	return "Function " + fn.Name.Name + "."
}
