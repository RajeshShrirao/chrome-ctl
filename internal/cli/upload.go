package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
	"github.com/spf13/cobra"
)

const uploadTimeout = 70 * time.Second

func newUploadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "upload <id> <file_path>",
		Short: "Upload a local file to a file input element",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseActionID(args[0])
			if err != nil {
				return err
			}

			path := args[1]
			info, err := os.Stat(path)
			if err != nil {
				return fmt.Errorf("cannot upload %q: %w", path, err)
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("cannot upload %q: not a regular file", path)
			}

			absPath, err := filepath.Abs(path)
			if err != nil {
				return fmt.Errorf("resolve %q: %w", path, err)
			}

			params := protocol.ActionUploadParams{ID: id, FilePath: absPath}
			var result protocol.ActionUploadResult
			if err := callRPCWithTimeout(cmd, protocol.MethodActionUpload, params, &result, uploadTimeout); err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), formatUpload(filepath.Base(absPath), info.Size(), id))
			return nil
		},
	}
}

func formatUpload(name string, size int64, id int) string {
	return fmt.Sprintf("OK: uploaded %s (%s) to [%d]\n", name, formatBytes(size), id)
}

// formatBytes renders a byte count in B, KB, or MB depending on magnitude.
func formatBytes(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
}
