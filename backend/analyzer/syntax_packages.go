package analyzer

import (
	"context"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func moduleDirectory(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("module path must be a directory: %s", dir)
	}
	info, err = os.Stat(filepath.Join(dir, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("read go.mod: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("go.mod must be a file")
	}
	return dir, nil
}

func readModulePath(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "module ") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "module "))
		value = strings.TrimSpace(strings.SplitN(value, "//", 2)[0])
		fields := strings.Fields(value)
		if len(fields) == 0 {
			break
		}
		modulePath := fields[0]
		if strings.HasPrefix(modulePath, "\"") || strings.HasPrefix(modulePath, "`") {
			modulePath, err = strconv.Unquote(modulePath)
			if err != nil {
				return "", fmt.Errorf("parse module path: %w", err)
			}
		}
		if modulePath == "" {
			break
		}
		return modulePath, nil
	}
	return "", fmt.Errorf("go.mod has no module directive")
}

func loadSyntaxPackages(ctx context.Context, root, modulePath string) ([]*syntaxPackage, error) {
	buildContext := build.Default
	buildContext.CgoEnabled = false
	fset := token.NewFileSet()
	byDir := map[string]*syntaxPackage{}
	err := filepath.WalkDir(root, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			if filename != root {
				name := entry.Name()
				if name == "vendor" || name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
					return filepath.SkipDir
				}
				if _, err := os.Stat(filepath.Join(filename, "go.mod")); err == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if filepath.Ext(filename) != ".go" || strings.HasSuffix(filename, "_test.go") {
			return nil
		}
		match, err := buildContext.MatchFile(filepath.Dir(filename), filepath.Base(filename))
		if err != nil {
			return fmt.Errorf("match Go file %s: %w", filename, err)
		}
		if !match {
			return nil
		}
		source, err := os.ReadFile(filename)
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(fset, filename, source, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("parse Go file %s: %w", filename, err)
		}
		if hasCgoImport(file) {
			return nil
		}
		relativeDir, err := filepath.Rel(root, filepath.Dir(filename))
		if err != nil {
			return err
		}
		packagePath := modulePath
		if relativeDir != "." {
			packagePath = path.Join(modulePath, filepath.ToSlash(relativeDir))
		}
		pkg := byDir[filepath.Dir(filename)]
		if pkg == nil {
			pkg = &syntaxPackage{path: packagePath, name: file.Name.Name, dir: filepath.Dir(filename), functions: map[string]*syntaxFunction{}, methods: map[string]map[string]*syntaxFunction{}, types: map[string]*syntaxType{}, names: map[string]bool{}, valueTypes: map[string]typeBinding{}}
			byDir[pkg.dir] = pkg
		} else if pkg.name != file.Name.Name {
			return fmt.Errorf("multiple package names in %s: %s and %s", pkg.dir, pkg.name, file.Name.Name)
		}
		relative, err := filepath.Rel(root, filename)
		if err != nil {
			return err
		}
		pkg.files = append(pkg.files, &syntaxFile{ast: file, fset: fset, source: source, relative: filepath.ToSlash(relative), pkg: pkg, imports: map[string]string{}})
		return nil
	})
	if err != nil {
		return nil, err
	}
	pkgs := make([]*syntaxPackage, 0, len(byDir))
	for _, pkg := range byDir {
		sort.Slice(pkg.files, func(i, j int) bool { return pkg.files[i].relative < pkg.files[j].relative })
		pkgs = append(pkgs, pkg)
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].path < pkgs[j].path })
	return pkgs, nil
}

func hasCgoImport(file *ast.File) bool {
	for _, spec := range file.Imports {
		if spec.Path.Value == "\"C\"" {
			return true
		}
	}
	return false
}
