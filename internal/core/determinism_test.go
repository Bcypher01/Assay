package core

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"strings"
	"testing"
)

// TestDeterminism runs the same operations twice and requires identical
// results. Every batch carries one of these; see CLAUDE.md.
func TestDeterminism(t *testing.T) {
	run := func() []int64 {
		var out []int64
		s := buildSeries(1000)
		v := s.Slice(s.Time[100], s.Time[900])
		out = append(out, int64(v.Len()))
		for i := 0; i < v.Len(); i += 97 {
			out = append(out, int64(v.Close[i]))
		}
		b := NewBitset(5000)
		for i := 0; i < 5000; i += 13 {
			b.Set(i)
		}
		out = append(out, int64(b.Count()))
		for i, ok := b.Next(0); ok; i, ok = b.Next(i + 1) {
			out = append(out, int64(i))
		}
		for _, tf := range []TF{TFM1, TFM15, TFH4, TFD1, TFW1} {
			out = append(out, int64(tf.AlignDown(MustParseTime("2026-08-22T10:07:31Z"))))
		}
		d := Distance{UnitATR, RatioFromFloat(0.25)}
		out = append(out, int64(d.Ticks(10, 108532, 120)))
		return out
	}

	a, b := run(), run()
	if len(a) != len(b) {
		t.Fatalf("run lengths differ: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("run differed at index %d: %d vs %d", i, a[i], b[i])
		}
	}
}

// TestNoFloatInExportedAPI enforces the batch exit criterion mechanically:
// float64 must not appear in an exported signature except in the named
// conversion helpers. A rule that is only written down decays.
func TestNoFloatInExportedAPI(t *testing.T) {
	allowed := map[string]bool{
		"RatioFromFloat": true,
		"Float64":        true,
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parsing package: %v", err)
	}

	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || !fn.Name.IsExported() || allowed[fn.Name.Name] {
					continue
				}
				if hasFloat(fn.Type) {
					t.Errorf("%s: exported signature uses float64; prices and ratios are fixed point", fn.Name.Name)
				}
			}
		}
	}
}

func hasFloat(ft *ast.FuncType) bool {
	check := func(fl *ast.FieldList) bool {
		if fl == nil {
			return false
		}
		for _, f := range fl.List {
			if id, ok := f.Type.(*ast.Ident); ok && (id.Name == "float64" || id.Name == "float32") {
				return true
			}
		}
		return false
	}
	return check(ft.Params) || check(ft.Results)
}
