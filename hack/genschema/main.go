// genschema writes schema/report.schema.json from internal/report's types.
//
// Usage (from the module root):
//
//	go run ./hack/genschema          # regenerate the schema
//	go run ./hack/genschema --check  # fail if the committed schema is stale
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"

	"github.com/nebari-dev/provenance-collector/internal/reportschema"
)

func main() {
	check := flag.Bool("check", false, "exit non-zero if "+reportschema.Path+" is out of date instead of writing it")
	flag.Parse()

	want, err := reportschema.Generate()
	if err != nil {
		fmt.Fprintln(os.Stderr, "genschema:", err)
		os.Exit(1)
	}

	if *check {
		got, err := os.ReadFile(reportschema.Path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "genschema:", err)
			os.Exit(1)
		}
		if !bytes.Equal(got, want) {
			fmt.Fprintf(os.Stderr, "%s is out of date; run `go run ./hack/genschema` and commit the result\n", reportschema.Path)
			os.Exit(1)
		}
		return
	}

	if err := os.WriteFile(reportschema.Path, want, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "genschema:", err)
		os.Exit(1)
	}
	fmt.Println("wrote", reportschema.Path)
}
