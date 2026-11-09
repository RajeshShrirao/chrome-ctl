package cli

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
	"github.com/spf13/cobra"
)

func newClickAtCmd() *cobra.Command {
	var snap, double bool
	var fromShot, seed string
	cmd := &cobra.Command{
		Use:   "clickat <X,Y | X Y>",
		Short: "Click exact viewport CSS pixels or image coordinates with --from-shot",
		Long: "Click a point given in viewport CSS pixels, exactly as `snap` prints them.\n" +
			"Accepts either comma-separated (e.g. 65,238) or space-separated (e.g. 65 238) coordinates.\n" +
			"When --from-shot <path> is passed, coordinates are treated as screenshot pixels and\n" +
			"automatically converted to viewport pixels using the shot's scale metadata.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			x, y, err := parseCoordinates(args)
			if err != nil {
				return err
			}

			if fromShot != "" {
				x, y, err = convertShotCoordinates(fromShot, x, y)
				if err != nil {
					return err
				}
			}

			params := protocol.ActionClickParams{
				At:          &protocol.ActionCoordinates{X: x, Y: y},
				DoubleClick: double,
				Snap:        snap,
				Seed:        seed,
			}
			var result protocol.ActionClickResult
			if err := callRPCWithTimeout(cmd, protocol.MethodActionClick, params, &result, clickTimeout); err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), formatClick(result, double))
			return nil
		},
	}
	cmd.Flags().BoolVar(&snap, "snap", false, "Include a page snapshot after clicking")
	cmd.Flags().BoolVar(&double, "double", false, "Double-click instead of a single click")
	cmd.Flags().StringVar(&fromShot, "from-shot", "", "Convert coordinates from screenshot image pixels to viewport pixels")
	cmd.Flags().StringVar(&seed, "seed", "", "Deterministic seed for humanized cursor trajectory and landing point")
	return cmd
}

func parseCoordinates(args []string) (int, int, error) {
	var parts []string
	if len(args) == 2 {
		parts = []string{args[0], args[1]}
	} else {
		raw := strings.TrimSpace(args[0])
		parts = strings.FieldsFunc(raw, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t'
		})
	}

	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid coordinates %v: use X,Y or X Y (for example 65,238 or 65 238)", args)
	}
	x, errX := strconv.Atoi(strings.TrimSpace(parts[0]))
	y, errY := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errX != nil || errY != nil || x < 0 || y < 0 {
		return 0, 0, fmt.Errorf("invalid coordinates %v: X and Y must be non-negative integers", args)
	}
	return x, y, nil
}

type shotMetadata struct {
	ViewportWidth  int     `json:"viewportWidth"`
	ViewportHeight int     `json:"viewportHeight"`
	ImageWidth     int     `json:"imageWidth"`
	ImageHeight    int     `json:"imageHeight"`
	Scale          float64 `json:"scale"`
}

func convertShotCoordinates(shotPath string, imgX, imgY int) (int, int, error) {
	metaPath := shotPath + ".meta.json"
	data, err := os.ReadFile(metaPath)
	if err != nil {
		// Also check adjacent to abs path
		abs, absErr := filepath.Abs(shotPath)
		if absErr == nil {
			data, err = os.ReadFile(abs + ".meta.json")
		}
	}
	if err != nil {
		return 0, 0, fmt.Errorf("read screenshot metadata %s: %w", metaPath, err)
	}

	var meta shotMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return 0, 0, fmt.Errorf("parse screenshot metadata: %w", err)
	}

	if meta.Scale > 0 {
		vpX := int(math.Round(float64(imgX) / meta.Scale))
		vpY := int(math.Round(float64(imgY) / meta.Scale))
		return vpX, vpY, nil
	}

	if meta.ImageWidth > 0 && meta.ViewportWidth > 0 {
		vpX := int(math.Round(float64(imgX) * float64(meta.ViewportWidth) / float64(meta.ImageWidth)))
		vpY := int(math.Round(float64(imgY) * float64(meta.ViewportHeight) / float64(meta.ImageHeight)))
		return vpX, vpY, nil
	}

	return imgX, imgY, nil
}
