package generator

import (
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/tools/go/packages"
)

type sdkRequestBodyInfo struct {
	FieldName string
	TypeName  string
	Pointer   bool
}

var sdkRequestBodyInfoCache = map[string]sdkRequestBodyInfo{}
var sdkPackageDirCache = map[string]string{}

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
	cacheKey := importPath + "." + typeName
	if info, ok := sdkRequestBodyInfoCache[cacheKey]; ok {
		return info, nil
	}

	dir, err := resolveGoPackageDir(importPath)
	if err != nil {
		return sdkRequestBodyInfo{}, err
	}

	pkgs, err := loadSDKPackages(importPath, dir)
	if err != nil {
		return sdkRequestBodyInfo{}, err
	}

	for _, pkg := range pkgs {
		for _, file := range pkg.Syntax {
			for _, decl := range file.Decls {
				genDecl, ok := decl.(*ast.GenDecl)
				if !ok || genDecl.Tok != token.TYPE {
					continue
				}
				for _, spec := range genDecl.Specs {
					typeSpec, ok := spec.(*ast.TypeSpec)
					if !ok || typeSpec.Name.Name != typeName {
						continue
					}
					structType, ok := typeSpec.Type.(*ast.StructType)
					if !ok {
						return sdkRequestBodyInfo{}, fmt.Errorf("type %q in %q is not a struct", typeName, importPath)
					}
					info, err := extractSDKRequestBodyInfo(structType)
					if err != nil {
						return sdkRequestBodyInfo{}, fmt.Errorf("type %q in %q: %w", typeName, importPath, err)
					}
					sdkRequestBodyInfoCache[cacheKey] = info
					return info, nil
				}
			}
		}
	}

	return sdkRequestBodyInfo{}, fmt.Errorf("type %q not found in %q", typeName, importPath)
}

func loadSDKPackages(importPath, dir string) ([]*packages.Package, error) {
	config := &packages.Config{
		Mode: packages.NeedName | packages.NeedCompiledGoFiles | packages.NeedSyntax,
		Dir:  dir,
	}

	pkgs, err := packages.Load(config, ".")
	if err != nil {
		return nil, fmt.Errorf("load package %q from %q: %w", importPath, dir, err)
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("load package %q from %q: no packages returned", importPath, dir)
	}

	for _, pkg := range pkgs {
		if len(pkg.Errors) == 0 {
			continue
		}
		return nil, fmt.Errorf("load package %q from %q: %w", importPath, dir, pkg.Errors[0])
	}

	return pkgs, nil
}

func resolveGoPackageDir(importPath string) (string, error) {
	if dir, ok := sdkPackageDirCache[importPath]; ok {
		return dir, nil
	}

	if dir, err := resolveGoPackageDirFromModuleCache(importPath); err == nil {
		sdkPackageDirCache[importPath] = dir
		return dir, nil
	}

	cmd := exec.Command("go", "list", "-f", "{{.Dir}}", importPath)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go list %q: %w", importPath, err)
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		return "", fmt.Errorf("go list %q returned empty directory", importPath)
	}
	sdkPackageDirCache[importPath] = dir
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
		sort.Strings(matches)
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
