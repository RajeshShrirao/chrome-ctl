// Command protocolgen renders the wire contract into Go, TypeScript, and docs.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol/codegen"
)

type output struct {
	path   string
	render func() ([]byte, error)
}

func main() {
	contractPath := flag.String("contract", "contract.json", "path to the wire contract JSON")
	goPath := flag.String("go", "methods_generated.go", "path to the generated Go file")
	tsPath := flag.String("ts", "", "path to the generated TypeScript file")
	docsPath := flag.String("docs", "", "path to the generated markdown file")
	check := flag.Bool("check", false, "fail when generated files differ instead of writing")
	flag.Parse()

	contract, err := codegen.Load(*contractPath)
	if err != nil {
		fatal(err)
	}

	outputs := []output{{path: *goPath, render: contract.Go}}
	if *tsPath != "" {
		outputs = append(outputs, output{path: *tsPath, render: contract.TS})
	}
	if *docsPath != "" {
		outputs = append(outputs, output{path: *docsPath, render: contract.Docs})
	}

	failed := false
	for _, out := range outputs {
		rendered, err := out.render()
		if err != nil {
			fatal(err)
		}
		if *check {
			existing, err := os.ReadFile(out.path)
			if err != nil || !bytes.Equal(existing, rendered) {
				fmt.Fprintf(os.Stderr, "%s is out of date; run go generate ./internal/protocol\n", out.path)
				failed = true
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(out.path), 0o755); err != nil {
			fatal(err)
		}
		if err := os.WriteFile(out.path, rendered, 0o644); err != nil {
			fatal(err)
		}
	}
	if failed {
		os.Exit(1)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "protocolgen:", err)
	os.Exit(1)
}
