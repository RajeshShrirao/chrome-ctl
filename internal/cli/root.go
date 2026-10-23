package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// ExitError carries an explicit process exit code. Silent suppresses the error line.
type ExitError struct {
	Code   int
	Silent bool
	Err    error
}

func (e *ExitError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("exit status %d", e.Code)
}

func (e *ExitError) Unwrap() error { return e.Err }

func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "chrome-ctl",
		Short:         "Ultra-low token browser automation bridge for AI agents",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(
		newVersionCmd(),
		newStatusCmd(),
		newDaemonCmd(),
		newTabsCmd(),
		newOpenCmd(),
		newSwitchCmd(),
		newSnapCmd(),
		newReadCmd(),
		newClickCmd(),
		newTypeCmd(),
		newPressCmd(),
		newScrollCmd(),
		newUploadCmd(),
		newWaitCmd(),
		newShotCmd(),
		newRecCmd(),
		newSSOCmd(),
		newMacroCmd(),
	)
	return root
}

func Execute() int {
	err := NewRoot().Execute()
	if err == nil {
		return 0
	}

	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		if !exitErr.Silent {
			printError(exitErr)
		}
		return exitErr.Code
	}

	printError(err)
	return 1
}

func printError(err error) {
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
}
