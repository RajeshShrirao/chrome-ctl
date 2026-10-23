package cli

import (
	"fmt"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
	"github.com/spf13/cobra"
)

const (
	recStartTimeout = 30 * time.Second
	recStopTimeout  = 180 * time.Second
	defaultFPS      = 2
)

func newRecCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rec",
		Short: "Record the active tab as a storyboard or WebM",
	}
	cmd.AddCommand(newRecStartCmd(), newRecStopCmd())
	return cmd
}

func newRecStartCmd() *cobra.Command {
	var fps int
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start recording the active tab",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateFPS(fps); err != nil {
				return err
			}

			params := protocol.MediaRecStartParams{FPS: fps}
			var result protocol.MediaRecStartResult
			if err := callRPCWithTimeout(cmd, protocol.MethodMediaRecStart, params, &result, recStartTimeout); err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), formatRecStart(result))
			return nil
		},
	}
	cmd.Flags().IntVar(&fps, "fps", defaultFPS, "Frames per second (1-2)")
	return cmd
}

func newRecStopCmd() *cobra.Command {
	var grid bool
	var output string
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop recording and save a storyboard or WebM",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := resolveOutputPath(output)
			if err != nil {
				return err
			}

			params := protocol.MediaRecStopParams{Grid: grid, OutputPath: path}
			var result protocol.MediaRecStopResult
			if err := callRPCWithTimeout(cmd, protocol.MethodMediaRecStop, params, &result, recStopTimeout); err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), formatRecStop(result, grid))
			return nil
		},
	}
	cmd.Flags().BoolVar(&grid, "grid", false, "Save a storyboard grid instead of a WebM video")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output file path (default: daemon-chosen)")
	return cmd
}

func validateFPS(fps int) error {
	if fps < 1 || fps > 2 {
		return fmt.Errorf("fps must be between 1 and 2 (got %d)", fps)
	}
	return nil
}

func formatRecStart(result protocol.MediaRecStartResult) string {
	return fmt.Sprintf("OK: recording at %dfps\n", result.FPS)
}

func formatRecStop(result protocol.MediaRecStopResult, grid bool) string {
	if grid {
		return fmt.Sprintf("OK: saved %s (%d frames, %s grid, %s)\n",
			result.Path, result.FramesCaptured, result.GridDimensions, formatBytes(result.FileSizeBytes))
	}
	return fmt.Sprintf("OK: saved %s (%d frames, %.1fs, %s)\n",
		result.Path, result.FramesCaptured, float64(result.DurationMs)/1000, formatBytes(result.FileSizeBytes))
}
