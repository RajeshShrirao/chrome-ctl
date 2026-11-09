package protocol

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// The generated files are the only hand-maintained copy of the wire contract.
// protocol.ts must re-export them rather than redeclaring methods or codes.
func TestExtensionProtocolReExportsGeneratedContract(t *testing.T) {
	path := filepath.Join("..", "..", "extension", "src", "shared", "protocol.ts")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	block := regexp.MustCompile(`(?s)export \{(.*?)\} from "\./protocol\.generated\.(?:js|ts)";`).
		FindStringSubmatch(string(source))
	if block == nil {
		t.Fatalf("%s: generated re-export not found", path)
	}
	for _, name := range []string{"Methods", "ErrorCodes", "rpcTimeoutMs", "PROTOCOL_VERSION", "CAPABILITIES"} {
		if !regexp.MustCompile(`\b` + name + `\b`).MatchString(block[1]) {
			t.Errorf("%s does not re-export %s", path, name)
		}
	}

	generatedPath := filepath.Join("..", "..", "extension", "src", "shared", "protocol.generated.ts")
	generated, err := os.ReadFile(generatedPath)
	if err != nil {
		t.Fatalf("read %s: %v", generatedPath, err)
	}
	for _, name := range []string{"export const Methods", "export const ErrorCodes", "export function rpcTimeoutMs"} {
		if !regexp.MustCompile(regexp.QuoteMeta(name)).Match(generated) {
			t.Errorf("%s is missing %q", generatedPath, name)
		}
	}
}
