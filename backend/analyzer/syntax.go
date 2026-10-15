package analyzer

import (
	"context"
	"go/ast"
	"go/token"
	"sort"
)

type syntaxPackage struct {
	path       string
	name       string
	dir        string
	files      []*syntaxFile
	functions  map[string]*syntaxFunction
	methods    map[string]map[string]*syntaxFunction
	types      map[string]*syntaxType
	names      map[string]bool
	valueTypes map[string]typeBinding
}

type syntaxFile struct {
	ast        *ast.File
	fset       *token.FileSet
	source     []byte
	relative   string
	pkg        *syntaxPackage
	imports    map[string]string
	dotImports []string
}

type syntaxFunction struct {
	id          string
	node        *ast.FuncDecl
	file        *syntaxFile
	pkg         *syntaxPackage
	receiver    string
	receiverPtr bool
	shadows     map[string]bool
	typeParams  map[string]bool
	bindings    []syntaxBinding
}

type syntaxBinding struct {
	name   string
	start  int
	end    int
	typ    typeBinding
	isType bool
}

type syntaxType struct {
	spec  *ast.TypeSpec
	file  *syntaxFile
	group *ast.GenDecl
}

type typeKey struct {
	pkg  string
	name string
}

type typeReference struct {
	key           typeKey
	pointers      int
	args          string
	typeArguments []typeArgument
	displayName   string
}

type typeBinding struct {
	ref       typeReference
	known     bool
	ambiguous bool
}

type typeArgument struct {
	text  string
	ref   typeReference
	known bool
}

type syntaxState struct {
	graph       Graph
	modulePath  string
	packages    map[string]*syntaxPackage
	functions   []*syntaxFunction
	localTypes  map[typeKey]*syntaxType
	definitions map[typeKey]string
	interfaces  map[typeKey]bool
	diagnostics []string
}

type edgeAt struct {
	edge Edge
	pos  int
}

func AnalyzeModule(ctx context.Context, dir string, progress func(string)) (Graph, []string, error) {
	graph := Graph{
		Entrypoints: []string{},
		Functions:   []Function{},
		Structs:     []StructDefinition{},
		Creations:   []Creation{},
		Edges:       []Edge{},
	}
	root, err := moduleDirectory(dir)
	if err != nil {
		return graph, nil, err
	}
	modulePath, err := readModulePath(root)
	if err != nil {
		return graph, nil, err
	}
	progress("loading")
	pkgs, err := loadSyntaxPackages(ctx, root, modulePath)
	if err != nil {
		return graph, nil, err
	}
	progress("mapping")
	state := syntaxState{
		graph:       graph,
		modulePath:  modulePath,
		packages:    map[string]*syntaxPackage{},
		localTypes:  map[typeKey]*syntaxType{},
		definitions: map[typeKey]string{},
		interfaces:  map[typeKey]bool{},
	}
	for _, pkg := range pkgs {
		state.packages[pkg.path] = pkg
	}
	state.indexImports()
	state.indexTypes()
	state.indexFunctions()
	state.indexFunctionScopes()
	for _, fn := range state.functions {
		state.scanFunction(fn)
	}
	sort.Strings(state.graph.Entrypoints)
	sort.Slice(state.graph.Functions, func(i, j int) bool {
		return state.graph.Functions[i].ID < state.graph.Functions[j].ID
	})
	sort.Slice(state.graph.Structs, func(i, j int) bool {
		return state.graph.Structs[i].ID < state.graph.Structs[j].ID
	})
	sort.Strings(state.diagnostics)
	return state.graph, state.diagnostics, nil
}
