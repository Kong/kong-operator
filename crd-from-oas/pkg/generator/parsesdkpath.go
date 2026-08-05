package generator

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/mod/semver"
)

type sdkRequestBodyInfo struct {
	FieldName string
	TypeName  string
	Pointer   bool
}

// sdkCacheMu guards the caches below. Generation itself is single-goroutine,
// but generator tests call t.Parallel(), so these package-level caches need
// protection against concurrent access.
var sdkCacheMu sync.Mutex

var sdkPackageDirCache = map[string]string{}

// sdkTypeIndexCache maps an SDK import path to a type-name → declared-type
// index, built once per package the first time any type in it is looked up.
// The SDK's models/components package alone is ~1260 files; re-parsing it on
// every lookup (as go/packages.Load previously did) dominated generation
// runtime.
var sdkTypeIndexCache = map[string]map[string]ast.Expr{}

// ParseSDKTypePath splits a fully qualified SDK type path like
// "github.com/Kong/sdk-konnect-go/models/components.CreatePortal"
// into its import path and type name by splitting on the last ".".
func ParseSDKTypePath(path string) (importPath, typeName string, err error) {
	lastDot := strings.LastIndex(path, ".")
	if lastDot == -1 || lastDot == 0 || lastDot == len(path)-1 {
		return "", "", fmt.Errorf("invalid SDK type path %q: must be in format 'importpath.TypeName'", path)
	}
	return path[:lastDot], path[lastDot+1:], nil
}

// ParseSDKRequestBodyInfo inspects an SDK request struct type and returns the
// JSON request body field metadata identified by the `request:"..."` tag.
func ParseSDKRequestBodyInfo(importPath, typeName string) (sdkRequestBodyInfo, error) {
	structType, ok, err := sdkStructType(importPath, typeName)
	if err != nil {
		return sdkRequestBodyInfo{}, err
	}
	if !ok {
		return sdkRequestBodyInfo{}, fmt.Errorf("type %q not found in %q", typeName, importPath)
	}

	info, err := extractSDKRequestBodyInfo(structType)
	if err != nil {
		return sdkRequestBodyInfo{}, fmt.Errorf("type %q in %q: %w", typeName, importPath, err)
	}
	return info, nil
}

// ParseSDKUnionMemberFieldNames returns the struct field names tagged as union
// members on an SDK type. It returns an empty slice when the type is not a
// union wrapper.
func ParseSDKUnionMemberFieldNames(importPath, typeName string) ([]string, error) {
	structType, ok, err := sdkStructType(importPath, typeName)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return extractSDKUnionMemberFieldNames(structType), nil
}

// sdkStructType resolves typeName within importPath to its declared struct
// type, using a per-package AST index built once and cached across calls
// (see sdkTypeIndexCache). ok is false when the type is not declared in the
// package; err reports a type declared but not a struct, or a failure to
// resolve/parse the package.
func sdkStructType(importPath, typeName string) (*ast.StructType, bool, error) {
	dir, err := resolveGoPackageDir(importPath)
	if err != nil {
		return nil, false, err
	}

	index, err := sdkPackageTypeIndex(importPath, dir)
	if err != nil {
		return nil, false, err
	}

	expr, ok := index[typeName]
	if !ok {
		return nil, false, nil
	}
	structType, ok := expr.(*ast.StructType)
	if !ok {
		return nil, false, fmt.Errorf("type %q in %q is not a struct", typeName, importPath)
	}
	return structType, true, nil
}

// sdkPackageTypeIndex returns the cached type-name → declared-type index for
// the package at dir, building it on first use.
func sdkPackageTypeIndex(importPath, dir string) (map[string]ast.Expr, error) {
	sdkCacheMu.Lock()
	if index, ok := sdkTypeIndexCache[importPath]; ok {
		sdkCacheMu.Unlock()
		return index, nil
	}
	sdkCacheMu.Unlock()

	index, err := buildSDKTypeIndex(dir)
	if err != nil {
		return nil, fmt.Errorf("load package %q from %q: %w", importPath, dir, err)
	}

	sdkCacheMu.Lock()
	sdkTypeIndexCache[importPath] = index
	sdkCacheMu.Unlock()
	return index, nil
}

// buildSDKTypeIndex parses every top-level .go file (excluding tests) in dir
// with go/parser and indexes each declared type by name. This avoids the
// go/packages.Load + go-list-subprocess round trip, which is unnecessary here
// since only struct tags are read — no type checking or cross-file resolution
// is needed.
func buildSDKTypeIndex(dir string) (map[string]ast.Expr, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %q: %w", dir, err)
	}

	index := make(map[string]ast.Expr)
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		filePath := filepath.Join(dir, name)
		f, err := parser.ParseFile(fset, filePath, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("parse %q: %w", filePath, err)
		}
		for _, decl := range f.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.TYPE {
				continue
			}
			for _, spec := range genDecl.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if _, exists := index[typeSpec.Name.Name]; exists {
					continue
				}
				index[typeSpec.Name.Name] = typeSpec.Type
			}
		}
	}
	return index, nil
}

func resolveGoPackageDir(importPath string) (string, error) {
	sdkCacheMu.Lock()
	dir, ok := sdkPackageDirCache[importPath]
	sdkCacheMu.Unlock()
	if ok {
		return dir, nil
	}

	if dir, err := resolveGoPackageDirFromModuleCache(importPath); err == nil {
		sdkCacheMu.Lock()
		sdkPackageDirCache[importPath] = dir
		sdkCacheMu.Unlock()
		return dir, nil
	}

	cmd := exec.Command("go", "list", "-f", "{{.Dir}}", importPath)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go list %q: %w", importPath, err)
	}
	dir = strings.TrimSpace(string(out))
	if dir == "" {
		return "", fmt.Errorf("go list %q returned empty directory", importPath)
	}
	sdkCacheMu.Lock()
	sdkPackageDirCache[importPath] = dir
	sdkCacheMu.Unlock()
	return dir, nil
}

func resolveGoPackageDirFromModuleCache(importPath string) (string, error) {
	cmd := exec.Command("go", "env", "GOMODCACHE")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go env GOMODCACHE: %w", err)
	}
	modCache := strings.TrimSpace(string(out))
	if modCache == "" {
		return "", fmt.Errorf("go env GOMODCACHE returned empty path")
	}

	parts := strings.Split(importPath, "/")
	for i := len(parts); i >= 1; i-- {
		modulePath := strings.Join(parts[:i], "/")
		subdirParts := parts[i:]
		pattern := filepath.Join(modCache, escapeModuleCachePath(modulePath)+"@*")
		matches, globErr := filepath.Glob(pattern)
		if globErr != nil {
			return "", fmt.Errorf("glob %q: %w", pattern, globErr)
		}
		if len(matches) == 0 {
			continue
		}
		sortModuleCacheMatches(matches)
		for _, v := range slices.Backward(matches) {
			candidate := v
			if len(subdirParts) > 0 {
				candidate = filepath.Join(candidate, filepath.Join(subdirParts...))
			}
			info, statErr := os.Stat(candidate)
			if statErr == nil && info.IsDir() {
				return candidate, nil
			}
		}
	}

	return "", fmt.Errorf("package %q not found in module cache %q", importPath, modCache)
}

func escapeModuleCachePath(path string) string {
	var builder strings.Builder
	builder.Grow(len(path))
	for _, r := range path {
		if unicode.IsUpper(r) {
			builder.WriteByte('!')
			builder.WriteRune(unicode.ToLower(r))
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

func sortModuleCacheMatches(matches []string) {
	sort.SliceStable(matches, func(i, j int) bool {
		vi, vj := moduleCachePathVersion(matches[i]), moduleCachePathVersion(matches[j])
		if semver.IsValid(vi) && semver.IsValid(vj) && vi != vj {
			return semver.Compare(vi, vj) < 0
		}
		return matches[i] < matches[j]
	})
}

func moduleCachePathVersion(path string) string {
	idx := strings.LastIndex(path, "@")
	if idx == -1 || idx == len(path)-1 {
		return ""
	}
	return path[idx+1:]
}

func extractSDKRequestBodyInfo(structType *ast.StructType) (sdkRequestBodyInfo, error) {
	for _, field := range structType.Fields.List {
		if field.Tag == nil {
			continue
		}
		tag := reflect.StructTag(strings.Trim(field.Tag.Value, "`"))
		if tag.Get("request") == "" {
			continue
		}
		if len(field.Names) != 1 {
			return sdkRequestBodyInfo{}, fmt.Errorf("request body field must have exactly one name")
		}
		typeName, pointer, err := sdkFieldTypeName(field.Type)
		if err != nil {
			return sdkRequestBodyInfo{}, err
		}
		return sdkRequestBodyInfo{
			FieldName: field.Names[0].Name,
			TypeName:  typeName,
			Pointer:   pointer,
		}, nil
	}

	return sdkRequestBodyInfo{}, fmt.Errorf("request body field not found")
}

func extractSDKUnionMemberFieldNames(structType *ast.StructType) []string {
	var names []string
	for _, field := range structType.Fields.List {
		if field.Tag == nil {
			continue
		}
		tag := reflect.StructTag(strings.Trim(field.Tag.Value, "`"))
		if tag.Get("union") != "member" {
			continue
		}
		for _, name := range field.Names {
			names = append(names, name.Name)
		}
	}
	return names
}

func sdkFieldTypeName(expr ast.Expr) (string, bool, error) {
	switch typed := expr.(type) {
	case *ast.StarExpr:
		typeName, _, err := sdkFieldTypeName(typed.X)
		return typeName, true, err
	case *ast.SelectorExpr:
		return typed.Sel.Name, false, nil
	case *ast.Ident:
		return typed.Name, false, nil
	default:
		return "", false, fmt.Errorf("unsupported request body field type %T", expr)
	}
}
