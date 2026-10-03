// checkenvs is a static guard against env-var drift.
//
// It scans every .go file under cmd/ and internal/ for string literals that
// look like an env-var name this project might read (`PROVENANCE_*` or the
// well-known `KUBECONFIG`) and fails if any of those names isn't present in
// internal/configspec.Vars.
//
// We walk the AST rather than greping `os.Getenv("...")` because the
// collector reads env vars through helper functions (`envDefault`,
// `envBool`, `envDuration`) that take the name as a *parameter* — so the
// string literal sits at the call site, not inside `os.Getenv`. Comments
// and other non-string tokens are naturally excluded by parsing.
//
// The point isn't to forbid os.Getenv — it's to keep the spec the single
// source of truth for "what env vars does this project read". Adding a new
// env var means: add it to internal/configspec/spec.go, then read it in
// code. Forgetting the spec entry trips this check.
//
// Usage:
//
//	go run ./hack/checkenvs
//
// Exits 0 if everything reads through known names, 1 otherwise.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/nebari-dev/provenance-collector/internal/configspec"
)

// envNamePattern is what an env-var name from this project looks like — a
// PROVENANCE_* identifier or the well-known KUBECONFIG. Anchored so we don't
// match substrings.
var envNamePattern = regexp.MustCompile(`^(PROVENANCE_[A-Z0-9_]+|KUBECONFIG)$`)

// Directories to scan. We deliberately exclude hack/ — the spec is in
// internal/configspec but the tooling under hack/ doesn't read env vars
// directly (gendocs reads the spec; checkenvs is what you're reading).
var scanDirs = []string{"cmd", "internal"}

func main() {
	found, err := scanForEnvNames()
	if err != nil {
		fmt.Fprintln(os.Stderr, "checkenvs:", err)
		os.Exit(2)
	}

	spec := map[string]bool{}
	for _, v := range configspec.Vars {
		spec[v.Name] = true
	}

	var unknown []string
	for name := range found {
		if !spec[name] {
			unknown = append(unknown, name)
		}
	}
	sort.Strings(unknown)

	if len(unknown) > 0 {
		fmt.Fprintln(os.Stderr, "Env vars referenced in source but missing from internal/configspec.Vars:")
		for _, n := range unknown {
			fmt.Fprintln(os.Stderr, "  -", n)
		}
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Add each one to internal/configspec/spec.go with its Scope / Kind / Default / Description,")
		fmt.Fprintln(os.Stderr, "then run `make docs` to regenerate docs/configuration.md.")
		os.Exit(1)
	}

	fmt.Printf("checkenvs: %d env-var name(s) referenced in source, all covered by spec.\n", len(found))
}

func scanForEnvNames() (map[string]struct{}, error) {
	found := map[string]struct{}{}
	fset := token.NewFileSet()
	for _, dir := range scanDirs {
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return fmt.Errorf("parse %s: %w", path, err)
			}
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				// BasicLit.Value includes the surrounding quotes; trim them.
				val := strings.Trim(lit.Value, "`\"")
				if envNamePattern.MatchString(val) {
					found[val] = struct{}{}
				}
				return true
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return found, nil
}
