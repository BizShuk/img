package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/bizshuk/img/config"
	"github.com/bizshuk/img/svc"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	convertFormat    string
	convertOutput    string
	convertWidth     int
	convertHeight    int
	convertScale     float64
	convertMode      string
	convertUpscale   bool
	convertQuality   int
	convertMethod    int
	convertLossless  bool
	convertNoRotate  bool
	convertOverwrite bool
	convertJSON      bool
)

// ConvertCmd 在 jpg / png / webp 之間轉換並調整尺寸。
var ConvertCmd = &cobra.Command{
	Use:   "convert <path>... -f <format>",
	Short: "Convert images between webp, jpg and png, optionally resizing",
	Long: `在 jpg / png / webp 之間轉換影像，並可同時調整尺寸。

輸入接受多個檔案路徑，也接受目錄（只掃該層，不遞迴）。
-o 指向目錄時逐檔輸出並沿用原檔名；指向檔名時只允許單一輸入；
未給時輸出到原檔旁邊。預設不覆寫既有檔案。

尺寸：只給 --width 或 --height 時依原比例算出另一邊；兩邊都給時由
--mode 決定 fit（縮入框內）、cover（填滿並置中裁切）或 exact（拉伸變形）。
--scale 按倍率縮放，不能與 --width/--height 併用。預設不放大。`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(c *cobra.Command, args []string) error {
		req, err := buildConvertRequest(c)
		if err != nil {
			return err
		}

		results, errs := svc.Convert(args, req)

		if convertJSON {
			if err := writeResultJSON(c.OutOrStdout(), results); err != nil {
				return err
			}
		} else {
			writeResultTable(c.OutOrStdout(), results)
		}

		if len(results) == 0 && len(errs) > 0 {
			return errors.Join(errs...)
		}
		reportErrors(c.ErrOrStderr(), errs)
		return nil
	},
}

func init() {
	f := ConvertCmd.Flags()
	f.StringVarP(&convertFormat, "format", "f", "", "target format: webp, jpg or png (required)")
	f.StringVarP(&convertOutput, "output", "o", "", "output file or directory (default: alongside the source)")
	f.IntVarP(&convertWidth, "width", "w", 0, "target width in pixels")
	f.IntVarP(&convertHeight, "height", "H", 0, "target height in pixels")
	f.Float64Var(&convertScale, "scale", 0, "scale factor, e.g. 0.5 (cannot be combined with --width/--height)")
	f.StringVar(&convertMode, "mode", "", "how to fit both dimensions: fit, cover or exact (default from settings)")
	f.BoolVar(&convertUpscale, "upscale", false, "allow enlarging images smaller than the target")
	f.IntVarP(&convertQuality, "quality", "q", 0, "encode quality 1-100 for jpg and lossy webp (default from settings)")
	f.IntVar(&convertMethod, "method", 0, "webp compression effort 0-6, higher is slower and smaller (default from settings)")
	f.BoolVar(&convertLossless, "lossless", false, "encode webp losslessly, ignoring --quality")
	f.BoolVar(&convertNoRotate, "no-auto-rotate", false, "keep the stored pixel orientation instead of applying EXIF rotation")
	f.BoolVar(&convertOverwrite, "overwrite", false, "replace existing output files")
	f.BoolVar(&convertJSON, "json", false, "emit a JSON array instead of a table")

	if err := ConvertCmd.MarkFlagRequired("format"); err != nil {
		panic(err)
	}
}

// buildConvertRequest 把旗標與設定檔合併成一份請求。旗標沒被明確設定時
// 才回頭取 viper 的值，避免旗標的零值蓋掉使用者設定。
func buildConvertRequest(c *cobra.Command) (svc.ConvertRequest, error) {
	target, err := svc.ParseFormat(convertFormat)
	if err != nil {
		return svc.ConvertRequest{}, err
	}
	if convertLossless && target != svc.FORMAT_WEBP {
		return svc.ConvertRequest{}, fmt.Errorf("--lossless only applies to webp, not %s", target)
	}

	modeName := convertMode
	if !c.Flags().Changed("mode") {
		modeName = viper.GetString(config.KEY_RESIZE_MODE)
	}
	mode, err := svc.ParseMode(modeName)
	if err != nil {
		return svc.ConvertRequest{}, err
	}

	quality := convertQuality
	if !c.Flags().Changed("quality") {
		quality = viper.GetInt(config.KEY_QUALITY)
	}
	method := convertMethod
	if !c.Flags().Changed("method") {
		method = viper.GetInt(config.KEY_WEBP_METHOD)
	}

	resize := svc.ResizeSpec{
		Width:   convertWidth,
		Height:  convertHeight,
		Scale:   convertScale,
		Mode:    mode,
		Upscale: convertUpscale,
	}
	if err := resize.Validate(); err != nil {
		return svc.ConvertRequest{}, err
	}

	return svc.ConvertRequest{
		Target:     target,
		Resize:     resize,
		Encode:     svc.EncodeOptions{Quality: quality, Lossless: convertLossless, Method: method},
		AutoRotate: !convertNoRotate,
		Overwrite:  convertOverwrite,
		Output:     convertOutput,
	}, nil
}

func writeResultJSON(w io.Writer, results []svc.Result) error {
	if results == nil {
		results = []svc.Result{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(results)
}

func writeResultTable(w io.Writer, results []svc.Result) {
	for _, r := range results {
		fmt.Fprintf(w, "%s -> %s  %s %dx%d  %s -> %s (%s)\n",
			r.Source, r.Dest, r.Format, r.Width, r.Height,
			humanBytes(r.SourceBytes), humanBytes(r.Bytes), sizeDelta(r.SourceBytes, r.Bytes))
	}
}

// sizeDelta 以百分比表示檔案大小的增減。
func sizeDelta(before, after int64) string {
	if before <= 0 {
		return "n/a"
	}
	pct := (float64(after) - float64(before)) / float64(before) * 100
	return fmt.Sprintf("%+.0f%%", pct)
}
