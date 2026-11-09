package protocol

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol/codegen"
)

// TestGeneratedContractUpToDate fails when a committed generated artifact
// differs from contract.json. CI runs this through `go test ./...`.
func TestGeneratedContractUpToDate(t *testing.T) {
	contract, err := codegen.Load("contract.json")
	if err != nil {
		t.Fatalf("load contract: %v", err)
	}

	outputs := []struct {
		path   string
		render func() ([]byte, error)
	}{
		{"methods_generated.go", contract.Go},
		{filepath.Join("..", "..", "extension", "src", "shared", "protocol.generated.ts"), contract.TS},
		{filepath.Join("..", "..", "docs", "PROTOCOL_CONTRACT.md"), contract.Docs},
	}
	for _, output := range outputs {
		want, err := output.render()
		if err != nil {
			t.Fatalf("render %s: %v", output.path, err)
		}
		got, err := os.ReadFile(output.path)
		if err != nil {
			t.Fatalf("read %s: %v", output.path, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s is out of date; run go generate ./internal/protocol", output.path)
		}
	}
}

func TestContractTimeoutBudgets(t *testing.T) {
	if got := TimeoutMs(MethodPageWait); got != 45000 {
		t.Errorf("page.wait timeout = %d, want 45000 (extension and daemon must agree)", got)
	}
	if got := TimeoutMs(MethodPageEval); got != 35000 {
		t.Errorf("page.eval timeout = %d, want 35000", got)
	}
	if got := TimeoutMs("unknown.method"); got != 10000 {
		t.Errorf("default timeout = %d, want 10000", got)
	}
}

func TestContractMethodCoverage(t *testing.T) {
	contract, err := codegen.Load("contract.json")
	if err != nil {
		t.Fatalf("load contract: %v", err)
	}

	for _, m := range contract.Methods {
		if m.Name == "" {
			t.Errorf("empty method name in contract")
		}
		if TimeoutMs(m.Name) <= 0 {
			t.Errorf("TimeoutMs(%s) = %d, want > 0", m.Name, TimeoutMs(m.Name))
		}
	}
}
