package cli

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
	"github.com/spf13/cobra"
)

const (
	shotTimeout     = 60 * time.Second
	defaultQuality  = 65
	defaultMaxWidth = 1024
)

func newShotCmd() *cobra.Command {
	var som, gray bool
	var output string
	var quality, maxWidth int
	cmd := &cobra.Command{
		Use:   "shot",
		Short: "Capture a screenshot of the active tab",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateShotOptions(quality, maxWidth); err != nil {
				return err
			}

			path, err := resolveOutputPath(output)
			if err != nil {
				return err
			}

			params := protocol.MediaShotParams{
				Som:        som,
				Gray:       gray,
				Quality:    quality,
				MaxWidth:   maxWidth,
				OutputPath: path,
			}
			var result protocol.MediaShotResult
			if err := callRPCWithTimeout(cmd, protocol.MethodMediaShot, params, &result, shotTimeout); err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), formatShot(result))
			return nil
		},
	}
	cmd.Flags().BoolVar(&som, "som", false, "Overlay Set-of-Mark badges on interactive elements")
	cmd.Flags().BoolVar(&gray, "gray", false, "Save as grayscale")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output file path (default: daemon-chosen)")
	cmd.Flags().IntVar(&quality, "quality", defaultQuality, "Image quality (1-100)")
	cmd.Flags().IntVar(&maxWidth, "max-width", defaultMaxWidth, "Maximum image width in pixels (256-4096)")
	return cmd
}

func validateShotOptions(quality, maxWidth int) error {
	if quality < 1 || quality > 100 {
		return fmt.Errorf("quality must be between 1 and 100 (got %d)", quality)
	}
	if maxWidth < 256 || maxWidth > 4096 {
		return fmt.Errorf("max-width must be between 256 and 4096 (got %d)", maxWidth)
	}
	return nil
}

// resolveOutputPath turns a relative output path into an absolute path so the
// daemon writes relative to the caller's cwd. Empty means "daemon default".
func resolveOutputPath(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", path, err)
	}
	return abs, nil
}

func formatShot(result protocol.MediaShotResult) string {
	out := fmt.Sprintf("OK: saved %s (%dx%d, %s",
		result.Path, result.Width, result.Height, formatBytes(result.FileSizeBytes))
	if result.Gray {
		out += ", grayscale"
	}
	if result.SomBadgeCount > 0 {
		out += fmt.Sprintf(", %d badges", result.SomBadgeCount)
	}
	return out + ")\n"
}
