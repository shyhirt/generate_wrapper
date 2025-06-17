package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

var GoTypes = []string{
	"int", "int8", "int16", "int32", "int64",
	"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",

	"float32", "float64",

	"complex64", "complex128",

	"string", "byte", "rune",

	"bool",

	"error", "any",

	"unsafe.Pointer",
	"interface{}",
}

const wrapperTemplateCore = `package cache

import (
	"context"
	sqlc "telegcat-core/sql/database"
)

type ValKey interface {
	Set(ctx context.Context, key string, val []byte) error
	Get(ctx context.Context, key string) ([]byte, error)
	Del(ctx context.Context, key ...string) error
}

type CachedQueries struct {
	DB    *{{.PackageAlias}}.Queries
	KV ValKey
}

func NewCachedQueries(db *{{.PackageAlias}}.Queries, redisClient ValKey) *CachedQueries {
	return &CachedQueries{DB: db, KV: redisClient}
}`

const wrapperTemplate = `package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"telegcat-core/sql/models"
)
{{range .Methods}}
func (c *CachedQueries) {{.Name}}({{.ParamsStr}}) ({{.ResultsStr}}) { {{if .UseCache}}	
	key := fmt.Sprintf("{{.KeyFormat}}", {{.KeyArgs}})
	val, err := c.KV.Get(ctx, key)
	if err == nil && val != nil {
		var cached {{.FirstResult}}
		if err := json.Unmarshal(val, &cached); err == nil {
			return cached, nil
		}
	}
	{{end}}
	result, err := c.DB.{{.Name}}({{.ArgsStr}})	
	{{if .UseCache}}
	if err == nil {
		bytes, _ := json.Marshal(result)
		_ = c.KV.Set(ctx, key, bytes)
	}
	{{end}}
	return result, err
}
{{end}}

`

type Method struct {
	Name        string
	ParamsStr   string
	ArgsStr     string
	ResultsStr  string
	FirstResult string
	KeyFormat   string
	KeyArgs     string
	UseCache    bool
}

type TemplateData struct {
	PackagePath  string
	PackageAlias string
	Methods      []Method
}

func main() {
	if len(os.Args) < 3 {
		log.Fatalln("Usage: go run generate_wrapper.go <package-path> <out-path>")
	}
	inputPath := os.Args[1]
	outputPath := os.Args[2]
	pkgAlias := "sqlc"
	var files []string

	tmplCore := template.Must(template.New("wrapper").Parse(wrapperTemplateCore))

	var buf bytes.Buffer
	err := tmplCore.Execute(&buf, TemplateData{
		PackagePath:  "",
		PackageAlias: pkgAlias,
	})
	if err != nil {
		log.Fatalf("template error: %v", err)
	}

	err = os.WriteFile(fmt.Sprintf("%s/%s", outputPath, "core.go"), buf.Bytes(), 0644)
	if err != nil {
		log.Fatalf("write error: %v", err)
	}

	fmt.Printf("✅ Generated: %s\n", "core.go")

	err = filepath.WalkDir(inputPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".sql.go") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		log.Fatalln(err)
	}

	for _, file := range files {
		a := strings.Split(file, "/")
		if len(a) < 2 {
			continue
		}
		name := a[len(a)-1]
		src, err := os.ReadFile(file)
		if err != nil {
			log.Fatalf("failed to read: %v", err)
		}

		fset := token.NewFileSet()
		node, err := parser.ParseFile(fset, inputPath, src, parser.AllErrors)
		if err != nil {
			log.Fatalf("parse error: %v", err)
		}

		structs := collectStructs(node)
		for k, st := range structs {
			fmt.Println(k)
			for _, v := range st.Fields.List {
				for _, i := range v.Names {
					fmt.Println(lowerFirst(i.String()))
				}
			}
		}

		var methods []Method

		for _, decl := range node.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Recv.List == nil {
				continue
			}

			recv := fn.Recv.List[0].Type
			star, ok := recv.(*ast.StarExpr)
			if !ok {
				continue
			}

			if ident, ok := star.X.(*ast.Ident); !ok || ident.Name != "Queries" {
				continue
			}

			// Сбор параметров
			var paramStrs []string
			var argNames []string
			var keyFormatParts []string
			var keyArgs []string
			for _, field := range fn.Type.Params.List {
				typ := exprToString(field.Type)
				for _, name := range field.Names {
					paramStrs = append(paramStrs, fmt.Sprintf("%s %s", name.Name, typ))
					argNames = append(argNames, name.Name)

					if name.Name != "ctx" {
						keyFormatParts = append(keyFormatParts, "%v")
						keyArgs = append(keyArgs, name.Name)
					}
				}
			}

			// Сбор возвращаемых значений
			var resultStrs []string
			firstResult := "interface{}"
			if fn.Type.Results != nil {
				for i, field := range fn.Type.Results.List {
					rtype := exprToString(field.Type)
					resultStrs = append(resultStrs, rtype)
					if i == 0 {
						firstResult = fixName(rtype)
					}
				}
			}

			for k := range paramStrs {
				if k > 0 {
					paramStrs[k] = fixName(paramStrs[k])
				}
			}
			for k := range resultStrs {
				resultStrs[k] = fixName(resultStrs[k])
			}
			methods = append(methods, Method{
				Name:        fn.Name.Name,
				ParamsStr:   strings.Join(paramStrs, ", "),
				ArgsStr:     strings.Join(argNames, ", "),
				ResultsStr:  strings.Join(resultStrs, ", "),
				FirstResult: firstResult,
				KeyFormat:   "cache:" + fn.Name.Name + ":" + strings.Join(keyFormatParts, ":"),
				KeyArgs:     strings.Join(keyArgs, ", "),
				UseCache: !strings.HasPrefix(fn.Name.Name, "Set") &&
					!strings.HasPrefix(fn.Name.Name, "Insert") &&
					!strings.HasPrefix(fn.Name.Name, "Update") &&
					!strings.HasPrefix(fn.Name.Name, "SoftDelete") &&
					!strings.HasPrefix(fn.Name.Name, "Create") &&
					!strings.HasPrefix(fn.Name.Name, "Delete"),
			})
		}

		tmpl := template.Must(template.New("wrapper").Parse(wrapperTemplate))

		var buf bytes.Buffer
		err = tmpl.Execute(&buf, TemplateData{
			PackagePath:  "",
			PackageAlias: pkgAlias,
			Methods:      methods,
		})
		if err != nil {
			log.Fatalf("template error: %v", err)
		}
		outP := fmt.Sprintf("%s/%s", outputPath, name)
		_ = os.MkdirAll(filepath.Dir(outP), 0755)
		err = os.WriteFile(outP, buf.Bytes(), 0644)
		if err != nil {
			log.Fatalf("write error: %v", err)
		}

		fmt.Printf("✅ Generated: %s\n", outP)
	}
}
func exprToString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return exprToString(e.X) + "." + e.Sel.Name
	case *ast.StarExpr:
		return "*" + exprToString(e.X)
	case *ast.ArrayType:
		return "[]" + exprToString(e.Elt)
	case *ast.MapType:
		return "map[" + exprToString(e.Key) + "]" + exprToString(e.Value)
	case *ast.Ellipsis:
		return "..." + exprToString(e.Elt)
	default:
		return fmt.Sprintf("%T", expr)
	}
}

func fixName(rtype string) string {
	gt := false

	var args []string
	if strings.Contains(rtype, " ") {
		args = strings.Split(rtype, " ")
	}
	var firstResult = ""
	if len(args) >= 2 {
		rtype = args[1]
	}

	for _, v := range GoTypes {
		if v == rtype {
			gt = true
			break
		}
	}
	if gt {
		if len(args) >= 2 {
			return strings.Join(args, " ")
		}
		return rtype
	}

	if strings.Contains(rtype, "[]") {
		firstResult = "[]models." + strings.ReplaceAll(rtype, "[]", "")
	} else {
		firstResult = "models." + rtype
	}
	if len(args) >= 2 {
		args[1] = firstResult
		firstResult = strings.Join(args, " ")
	}

	return firstResult
}

func collectStructs(file *ast.File) map[string]*ast.StructType {
	structs := make(map[string]*ast.StructType)
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			typeSpec := spec.(*ast.TypeSpec)
			if structType, ok := typeSpec.Type.(*ast.StructType); ok {
				structs[typeSpec.Name.Name] = structType
			}
		}
	}
	return structs
}
func lowerFirst(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToLower(s[:1]) + s[1:]
}
