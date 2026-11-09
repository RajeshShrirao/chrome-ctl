package protocol

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// goMethods lists every wire method constant declared in messages.go.
var goMethods = []string{
	MethodSystemReady,
	MethodSystemPing,
	MethodSessionStatus,
	MethodWindowList,
	MethodWindowCreate,
	MethodWindowEnsure,
	MethodWindowUse,
	MethodWindowClose,
	MethodTabList,
	MethodTabOpen,
	MethodTabSwitch,
	MethodPageSnap,
	MethodPageRead,
	MethodPageWait,
	MethodPageEval,
	MethodPageAttr,
	MethodPageNet,
	MethodPageFormState,
	MethodActionClick,
	MethodActionType,
	MethodActionPress,
	MethodActionScroll,
	MethodActionUpload,
	MethodMediaShot,
	MethodMediaRecStart,
	MethodMediaRecStop,
	MethodMediaCapture,
	MethodAuthSSO,
	MethodPageDescribe,
	MethodPageResolve,
	MethodMacroStart,
	MethodMacroSave,
	MethodMacroList,
	MethodMacroRun,
}

// daemonLocalMethods are served by the daemon and never forwarded to the
// extension, so they do not appear in the TypeScript Methods map.
var daemonLocalMethods = map[string]bool{
	MethodMacroStart: true,
	MethodMacroSave:  true,
	MethodMacroList:  true,
	MethodMacroRun:   true,
}

func TestExtensionMethodMirror(t *testing.T) {
	path := filepath.Join("..", "..", "extension", "src", "shared", "protocol.ts")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	block := regexp.MustCompile(`(?s)export const Methods = \{(.*?)\} as const;`).
		FindStringSubmatch(string(source))
	if block == nil {
		t.Fatalf("%s: Methods map not found", path)
	}

	entry := regexp.MustCompile(`"([a-z]+\.[a-z_]+)"`)
	tsMethods := map[string]bool{}
	for _, match := range entry.FindAllStringSubmatch(block[1], -1) {
		tsMethods[match[1]] = true
	}

	goSet := map[string]bool{}
	for _, method := range goMethods {
		goSet[method] = true
		if daemonLocalMethods[method] {
			continue
		}
		if !tsMethods[method] {
			t.Errorf("Go method %q is missing from extension Methods", method)
		}
	}

	for method := range tsMethods {
		if !goSet[method] {
			t.Errorf("extension method %q has no Go constant", method)
		}
	}
}

func TestErrorCodeMirror(t *testing.T) {
	path := filepath.Join("..", "..", "extension", "src", "shared", "protocol.ts")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	block := regexp.MustCompile(`(?s)export const ErrorCodes = \{(.*?)\} as const;`).
		FindStringSubmatch(string(source))
	if block == nil {
		t.Fatalf("%s: ErrorCodes map not found", path)
	}

	entry := regexp.MustCompile(`([a-zA-Z]+):\s*(-?\d+)`)
	tsCodes := map[string]int{}
	for _, match := range entry.FindAllStringSubmatch(block[1], -1) {
		tsCodes[match[1]] = atoi(match[2])
	}

	want := map[string]int{
		"connection":             ErrConnection,
		"elementNotFound":        ErrElementNotFound,
		"elementNotInteractable": ErrElementNotInteractable,
		"tabNotFound":            ErrTabNotFound,
		"debuggerDetached":       ErrDebuggerDetached,
		"ssoTimeout":             ErrSSOTimeout,
		"unauthorized":           ErrUnauthorized,
		"navigationFailed":       ErrNavigationFailed,
		"macroStepFailed":        ErrMacroStepFailed,
		"dispatchedUnconfirmed":  ErrDispatchedUnconfirmed,
		"snapshotStale":          ErrSnapshotStale,
		"featureDisabled":        ErrFeatureDisabled,
		"methodNotFound":         ErrMethodNotFound,
	}
	for name, code := range want {
		if got, ok := tsCodes[name]; !ok || got != code {
			t.Errorf("extension ErrorCodes[%q] = %d (present %v), want %d", name, got, ok, code)
		}
	}
}

func atoi(value string) int {
	sign := 1
	if strings.HasPrefix(value, "-") {
		sign = -1
		value = value[1:]
	}
	n := 0
	for _, r := range value {
		n = n*10 + int(r-'0')
	}
	return sign * n
}
