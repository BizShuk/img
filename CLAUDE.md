# img — 技術脈絡 (Technical Context)

本檔記錄套件結構、外部相依的選型理由與容易踩到的實作細節。
功能說明與 CLI 用法由 [README.md](README.md) 單一擁有，本檔不重複。

## 目錄結構 (Directory Layout)

```tree
img/
├── main.go                   # config.Default() -> cmd.Execute()
├── cmd/                      # cobra 命令層：旗標解析、輸出排版
│   ├── root.go               # RootCmd，掛上 gosdk 的 ConfigCmd
│   ├── analyze.go            # analyze 子命令與表格／JSON 輸出
│   ├── convert.go            # convert 子命令，旗標與設定檔的合併
│   └── format.go             # humanBytes / humanCount 排版輔助
├── config/                   # 設定初始化的單一入口
│   ├── config.go             # gosdk config.Default + slog 等級
│   └── default_settings.json # 首次執行落在 ~/.config/img/ 的種子
├── svc/                      # 影像領域邏輯，不依賴 cobra 或 viper
│   ├── format.go             # Format 列舉、名稱正規化、副檔名對應
│   ├── codec.go              # Decode / Encode 分派、alpha 壓平
│   ├── meta.go               # 三種容器的檔頭走訪
│   ├── exif.go               # orientation 讀取與套用
│   ├── resize.go             # fit / cover / exact 三模式
│   ├── analyze.go            # Info 與 Inspect
│   └── convert.go            # 轉換管線、輸入展開、輸出路徑解析
├── skills/img/SKILL.md       # 給 AI agent 的使用說明
└── docs/                     # terminology、memory
```

分層規則：`cmd/` 只做旗標與輸出，`svc/` 不 import cobra / viper。
設定值只在 `cmd/` 讀取後塞進 `svc.ConvertRequest`，`svc/` 對 viper 一無所知。

## 外部相依 (Dependencies)

| 套件 | 用途 | 選它的理由 |
| :--- | :--- | :--- |
| `github.com/bizshuk/gosdk` | 設定載入、`config` 子命令 | 家族慣例；`~/.config/img/` 由 `WithAppName` 決定 |
| `github.com/gen2brain/webp` | WebP 編解碼 | 唯一同時支援 `lossy + lossless encode` 且`免 cgo` 的選項 |
| `golang.org/x/image/draw` | 縮放重取樣 (CatmullRom) | 官方套件；`disintegration/imaging` 已停止維護 |
| `spf13/cobra` + `spf13/viper` | CLI 與設定 | gosdk 本來就用這組 |

### WebP 為什麼是 gen2brain

`x/image/webp` 只能 decode，`HugoSmits86/nativewebp` 只能 lossless encode，
`chai2010/webp` 與 `kolesa-team/go-webp` 都要 cgo 與系統 libwebp。

`gen2brain/webp` 走兩條路：系統有 libwebp 時用 `purego` dlopen 走快路徑，
沒有時 fallback 到 repo 內建、由 wasm 轉譯成 Go 的實作。代價是
`libwebp.go` 有 2.7 MB 原始碼，執行檔約 10-15 MB —— 換來`零外部二進位相依`。

`驗證純 Go 路徑：` `go test -tags nodynamic ./svc/` 會關掉 dlopen，
確認沒裝 libwebp 的機器也編得出 webp。

## 關鍵決策 (Key Decisions)

### 格式一律由 magic bytes 判定

`image.DecodeConfig` / `image.Decode` 回傳的 registered format name 就是判準。
`Format` 的字面值刻意與這些名稱一致（`jpeg` / `png` / `webp`），可以直接轉型。
副檔名`只`用來計算 `Info.Mismatch`，絕不參與解碼決策。

`唯一的例外`是 `svc.ExpandInputs` 掃描目錄時：那裡用副檔名過濾，
純粹是為了不去開啟目錄下每一個無關檔案。被選中的檔案仍走 magic bytes。

### 自寫最小 EXIF orientation 解析

`svc/exif.go` 的 `orientationFromTIFF` 只讀 IFD0 的 tag `0x0112`，約 60 行。
為了一個 tag 引入 `dsoprea/go-exif`（體積大）或 `rwcarlsen/goexif`（已封存）
不划算。WebP 的 EXIF chunk 與 JPEG 的 APP1 payload 共用同一個 TIFF 解析器。

### 三種容器各自的檔頭走訪

`svc/meta.go` 補的是 `image.Config` 給不出來的東西：位元深度、透明通道、
動畫張數、orientation。三種容器的結構完全不同，`最容易寫反的是 RIFF`：

- `PNG` 是 `length(4) + type(4) + payload`
- `RIFF (WebP)` 是 `FourCC(4) + length(4) + payload`，`順序相反`，
  且奇數長度的 chunk 後面要補一個 pad byte
- `JPEG` 是 marker 串列；`0xD0-0xD9`、`0x01`、`0xFF` 沒有長度欄位，
  `0xC4` / `0xC8` / `0xCC` 落在 SOF 的區間裡但不是 SOF

### 轉 JPEG 前壓白底

`image/jpeg` 對帶 alpha 的影像會直接忽略 alpha 通道，透明區域的
RGB 殘值會變成一片雜色。`svc.FlattenAlpha` 先把影像疊在白底上再編碼。
已經不透明的影像（`Opaque()` 為真）原樣通過，不多花一次複製。

### 先寫暫存檔再改名

`svc.ConvertFile` 一律寫到同目錄的暫存檔再 `os.Rename`。
編碼途中失敗不會留下半個檔案，`--overwrite` 原地轉換也不會先毀掉來源。

### 設定鍵用扁平的 SCREAMING_SNAKE

`QUALITY`、`WEBP_METHOD`、`RESIZE_MODE`、`LOG_LEVEL` 全是扁平鍵，
才能被 `APP_QUALITY` 這類環境變數覆寫；viper 的巢狀鍵含有 `.`，
對應不到合法的 shell 變數名。

### 旗標與設定的合併時機

cobra 的 `init()` 在 `main()` 之前跑，那時 viper 還沒載入，
所以`旗標預設值不能寫成設定檔的值`。`cmd/convert.go` 的
`buildConvertRequest` 改在 `RunE` 裡用 `Flags().Changed(name)` 判斷：
使用者沒明確給旗標時才回頭讀 viper。

## 開發指南 (Development Guide)

```bash
npm run lint     # gofmt -l . && go vet ./...
npm run test     # go test ./...
npm run build    # bin/img
npm run deploy   # go install .
```

測試`不放二進位 fixture`：需要的圖都由 `svc/helper_test.go` 的 `makeImage`
即時產生，帶 EXIF 的 JPEG 由 `jpegWithOrientation` 手工組出 APP1 段落。
新增格式時，`TestConvertRoundTripsEveryFormatPair` 會自動涵蓋所有新的組合。

`cmd/` 的測試要注意：旗標綁在套件層變數上，且 cobra 的子命令一律經由
根命令解析，因此 `cmd_test.go` 的 `run` 每次執行前會把旗標歸零。
`TestMain` 只設 viper 預設值，`不呼叫 config.Default` —— 那會在使用者的
`~/.config/img/` 落下檔案。
