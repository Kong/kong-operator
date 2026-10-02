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

// ParseSDKUnionMemberTypeNames returns the type names of the struct fields
// tagged as union members on an SDK type. It returns an empty slice when the
// type is not a union wrapper.
func ParseSDKUnionMemberTypeNames(importPath, typeName string) ([]string, error) {
	structType, ok, err := sdkStructType(importPath, typeName)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return extractSDKUnionMemberTypeNames(structType)
}

// sdkStructHasField reports whether the SDK struct typeName in importPath
// declares a field named fieldName.
func sdkStructHasField(importPath, typeName, fieldName string) (bool, error) {
	structType, ok, err := sdkStructType(importPath, typeName)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, fmt.Errorf("type %q not found in %q", typeName, importPath)
	}
	for _, field := range structType.Fields.List {
		for _, name := range field.Names {
			if name.Name == fieldName {
				return true, nil
			}
		}
	}
	return false, nil
}

// sdkStructFieldIsStringMap reports whether the field fieldName of the SDK
// components struct typeName is a map[string]string. It errors when the type
// or field is not found.
func sdkStructFieldIsStringMap(typeName, fieldName string) (bool, error) {
	const importPath = "github.com/Kong/sdk-konnect-go/models/components"
	structType, ok, err := sdkStructType(importPath, typeName)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, fmt.Errorf("type %q not found in %q", typeName, importPath)
	}
	for _, field := range structType.Fields.List {
		for _, name := range field.Names {
			if name.Name != fieldName {
				continue
			}
			mapType, isMap := field.Type.(*ast.MapType)
			if !isMap {
				return false, nil
			}
			key, keyOK := mapType.Key.(*ast.Ident)
			value, valueOK := mapType.Value.(*ast.Ident)
			return keyOK && valueOK && key.Name == "string" && value.Name == "string", nil
		}
	}
	return false, fmt.Errorf("field %q not found on type %q in %q", fieldName, typeName, importPath)
}

// sdkSliceFieldElemTypeName returns the element type name of the slice field
// fieldName on the SDK struct typeName in importPath. ok is false when the
// type is not declared in the package, or has no such slice field.
func sdkSliceFieldElemTypeName(importPath, typeName, fieldName string) (string, bool, error) {
	structType, ok, err := sdkStructType(importPath, typeName)
	if err != nil || !ok {
		return "", false, err
	}
	for _, field := range structType.Fields.List {
		for _, name := range field.Names {
			if name.Name != fieldName {
				continue
			}
			arrayType, isArray := field.Type.(*ast.ArrayType)
			if !isArray {
				return "", false, nil
			}
			elemTypeName, _, err := sdkFieldTypeName(arrayType.Elt)
			if err != nil {
				return "", false, err
			}
			return elemTypeName, true, nil
		}
	}
	return "", false, nil
}

// sdkStructFieldTypeName returns the type name (without a pointer) of the
// field fieldName on the SDK struct typeName in importPath. ok is false when
// the type is not declared in the package, or has no such field.
func sdkStructFieldTypeName(importPath, typeName, fieldName string) (string, bool, error) {
	structType, ok, err := sdkStructType(importPath, typeName)
	if err != nil || !ok {
		return "", false, err
	}
	for _, field := range structType.Fields.List {
		for _, name := range field.Names {
			if name.Name != fieldName {
				continue
			}
			fieldTypeName, _, err := sdkFieldTypeName(field.Type)
			if err != nil {
				return "", false, fmt.Errorf("field %q of type %q in %q: %w", fieldName, typeName, importPath, err)
			}
			return fieldTypeName, true, nil
		}
	}
	return "", false, nil
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

	// Prefer the version pinned by a module graph we belong to (walking up to
	// find one, e.g. the repository root module). The module-cache scan below
	// only sees whatever versions happen to be cached and picks the newest,
	// which can be a release that predates or postdates the pinned version —
	// e.g. the repo pins a v0.69.0-dev.2 prerelease while a v0.69.0 release
	// without the same types is also cached.
	if dir, err := resolveGoPackageDirFromGoMod(importPath); err == nil {
		sdkCacheMu.Lock()
		sdkPackageDirCache[importPath] = dir
		sdkCacheMu.Unlock()
		return dir, nil
	}

	if dir, err := resolveGoPackageDirFromModuleCache(importPath); err == nil {
		sdkCacheMu.Lock()
		sdkPackageDirCache[importPath] = dir
		sdkCacheMu.Unlock()
		return dir, nil
	}

	return "", fmt.Errorf("package %q not found in module graph or module cache", importPath)
}

// resolveGoPackageDirFromGoMod resolves importPath using the module graph of
// the nearest enclosing module that has the package's module in its build
// list, honoring replace directives. It walks up from the working directory.
func resolveGoPackageDirFromGoMod(importPath string) (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			cmd := exec.Command("go", "list", "-f", "{{.Dir}}", importPath)
			cmd.Dir = dir
			// Surface transient go list failures (e.g. a module download
			// blocked by the sandbox) instead of silently degrading to the
			// newest version found in the module cache.
			cmd.Stderr = os.Stderr
			if out, err := cmd.Output(); err == nil {
				if packageDir := strings.TrimSpace(string(out)); packageDir != "" {
					return packageDir, nil
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no module in the working directory or its parents provides %q", importPath)
		}
		dir = parent
	}
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

func extractSDKUnionMemberTypeNames(structType *ast.StructType) ([]string, error) {
	var names []string
	for _, field := range structType.Fields.List {
		if field.Tag == nil {
			continue
		}
		tag := reflect.StructTag(strings.Trim(field.Tag.Value, "`"))
		if tag.Get("union") != "member" {
			continue
		}
		typeName, _, err := sdkFieldTypeName(field.Type)
		if err != nil {
			return nil, err
		}
		names = append(names, typeName)
	}
	return names, nil
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
