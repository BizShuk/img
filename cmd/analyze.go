package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/bizshuk/img/svc"
	"github.com/spf13/cobra"
)

var analyzeJSON bool

// AnalyzeCmd 只讀檔頭並回報影像屬性，不解碼像素。
var AnalyzeCmd = &cobra.Command{
	Use:   "analyze <path>...",
	Short: "Report image format, dimensions and pixel properties",
	Long: `讀取檔頭並回報影像屬性：格式、寬高、總像素數、長寬比、color model、
位元深度、透明通道、EXIF orientation、動畫張數與檔案大小。

格式以 magic bytes 判定；副檔名與實際內容不符時會標示 mismatch。
heic 與 avif 讀不開，但仍會回報格式名與檔案大小，並把 decodable 標成 false。
--json 供腳本與 AI 消費。`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(c *cobra.Command, args []string) error {
		infos, errs := svc.InspectAll(args)

		if analyzeJSON {
			if err := writeJSON(c.OutOrStdout(), infos); err != nil {
				return err
			}
		} else {
			writeInfoTable(c.OutOrStdout(), infos)
		}

		if len(infos) == 0 && len(errs) > 0 {
			return errors.Join(errs...)
		}
		reportErrors(c.ErrOrStderr(), errs)
		return nil
	},
}

func init() {
	AnalyzeCmd.Flags().BoolVar(&analyzeJSON, "json", false, "emit a JSON array instead of a table")
}

func writeJSON(w io.Writer, infos []svc.Info) error {
	if infos == nil {
		infos = []svc.Info{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(infos)
}

func writeInfoTable(w io.Writer, infos []svc.Info) {
	for i, info := range infos {
		if i > 0 {
			fmt.Fprintln(w)
		}
		row(w, "file", info.Path)
		row(w, "format", string(info.Format))
		if !info.Decodable {
			row(w, "bytes", humanBytes(info.Bytes))
			row(w, "note", fmt.Sprintf("recognised by magic bytes; img cannot decode %s, so no dimensions are available", info.Format))
			if info.Mismatch {
				row(w, "mismatch", fmt.Sprintf("extension says %s but content is %s", info.ExtFormat, info.Format))
			}
			continue
		}
		row(w, "size", fmt.Sprintf("%d x %d (%s px)", info.Width, info.Height, humanCount(info.Pixels)))
		row(w, "ratio", info.Ratio)
		row(w, "color", fmt.Sprintf("%s / %d-bit / %s", info.ColorModel, info.BitDepth, alphaLabel(info.HasAlpha)))
		if info.Orientation != svc.ORIENTATION_NONE {
			row(w, "exif", fmt.Sprintf("orientation=%d", info.Orientation))
		}
		if info.Frames > 1 {
			row(w, "frames", fmt.Sprintf("%d (animated)", info.Frames))
		}
		if info.Progressive {
			row(w, "encoding", "progressive")
		}
		row(w, "bytes", humanBytes(info.Bytes))
		if info.Mismatch {
			row(w, "mismatch", fmt.Sprintf("extension says %s but content is %s", info.ExtFormat, info.Format))
		}
	}
}

func row(w io.Writer, key, value string) {
	fmt.Fprintf(w, "%-10s%s\n", key, value)
}

func alphaLabel(has bool) string {
	if has {
		return "alpha"
	}
	return "no alpha"
}

func reportErrors(w io.Writer, errs []error) {
	for _, err := range errs {
		fmt.Fprintln(w, "error:", err)
	}
}
