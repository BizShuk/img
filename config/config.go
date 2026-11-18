// Package config 是 img 設定的單一初始化入口。設定值全部走 viper，
// 不另立全域設定結構 —— 這個 CLI 的設定只有四個純量鍵。
package config

import (
	_ "embed"
	"log/slog"
	"os"
	"strings"

	gosdkcmdconfig "github.com/bizshuk/gosdk/cmd/config"
	sdkconfig "github.com/bizshuk/gosdk/config"
	"github.com/spf13/viper"
)

//go:embed example/settings.example.json
var defaultSettingsJSON string

// init 登記內嵌範例, 供 `config default` 複製到設定目錄.
func init() {
	gosdkcmdconfig.MustRegisterDefault("settings.json", []byte(defaultSettingsJSON))
}

// 設定鍵一律用扁平的 snake_case 命名；viper 查環境變數時會轉大寫，
// 因此同名 SCREAMING_SNAKE 環境變數（如 QUALITY）仍可覆寫。
// viper 的巢狀鍵（含「.」）無法對應到合法的 shell 變數名。
const (
	KEY_QUALITY     = "quality"
	KEY_WEBP_METHOD = "webp_method"
	KEY_RESIZE_MODE = "resize_mode"
	KEY_LOG_LEVEL   = "log_level"
)

// Default 載入設定並套用日誌等級。首次執行時 gosdk 會在
// ~/.config/img/ 落下 settings.json。
func Default() {
	sdkconfig.Default(
		sdkconfig.WithAppName("img"),
		sdkconfig.WithDefaultValue(defaultSettingsJSON),
	)

	viper.SetDefault(KEY_QUALITY, 82)
	viper.SetDefault(KEY_WEBP_METHOD, 4)
	viper.SetDefault(KEY_RESIZE_MODE, "fit")
	viper.SetDefault(KEY_LOG_LEVEL, "warn")

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel(viper.GetString(KEY_LOG_LEVEL)),
	})))
}

// logLevel 把設定字串轉成 slog 等級，無法辨識時退回 warn。
func logLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "error":
		return slog.LevelError
	default:
		return slog.LevelWarn
	}
}
