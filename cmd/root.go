package cmd

import (
	"fmt"
	"os"

	gosdkcmd "github.com/bizshuk/gosdk/cmd"
	"github.com/spf13/cobra"
)

// RootCmd 是 CLI 的根命令。img 本身不做事，所有功能都在子命令上。
var RootCmd = &cobra.Command{
	Use:   "img",
	Short: "Image conversion and inspection CLI",
	Long: `img 在 jpg / png / webp 之間轉換影像並調整尺寸，或只讀檔頭回報影像屬性。

格式一律以 magic bytes 判定，與副檔名無關。WebP 走純 Go 路徑，
不需要另外安裝 libwebp 或 ImageMagick。`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute 執行根命令。
func Execute() {
	if err := RootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func init() {
	RootCmd.AddCommand(gosdkcmd.ConfigCmd, AnalyzeCmd, ConvertCmd)
}
