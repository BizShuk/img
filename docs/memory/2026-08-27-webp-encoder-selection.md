# 2026-08-27 — WebP 編碼器選型與 RIFF 欄位順序

## 背景 (Context)

`img` 首版落地。硬性前提是`零外部二進位相依`：使用者不該為了轉一張 webp
去裝 ImageMagick 或 libwebp。這個前提直接決定了編碼器的選擇。

## WebP 編碼器：四個選項只有一個能用

| 套件 | encode | cgo | 結論 |
| :--- | :--- | :--- | :--- |
| `golang.org/x/image/webp` | `不支援` | 否 | 只能 decode，出局 |
| `HugoSmits86/nativewebp` | 只有 lossless | 否 | 照片轉出來體積過大，出局 |
| `chai2010/webp` | 完整 | `是` | 需要 cgo 與系統 libwebp，違反前提 |
| `gen2brain/webp` | 完整 | 否 | `採用` |

`gen2brain/webp` 的機制值得記住：它同時帶兩套實作。系統有 libwebp 時用
`ebitengine/purego` 直接 dlopen（不經 cgo），沒有時 fallback 到 repo 內建、
由 wasm 轉譯成 Go 的 `libwebp.go`（2.7 MB 原始碼）。

`要驗證純 Go 路徑真的能跑，用 nodynamic build tag：`

```bash
go test -tags nodynamic ./svc/
```

沒有這條驗證，「免相依」只是假設 —— 開發機上裝過 libwebp 的話，
測試會一路走 dlopen 快路徑，把 fallback 壞掉這件事完全遮住。

## RIFF 的欄位順序與 PNG 相反

實作 `svc/meta.go` 時踩到的坑。兩種容器的 chunk header 長得很像，順序卻相反：

```text
PNG   : length(4, big endian)    + type(4)   + payload
RIFF  : FourCC(4)                + length(4, little endian) + payload
```

寫反的症狀`不是 crash，是靜默錯誤`：chunk 名稱讀成長度、長度讀成名稱，
迴圈在第一次迭代就 `break`，於是每一張 WebP 都回報「沒有透明通道」。
`analyze` 照樣輸出一份看起來完全正常的報告。

抓到它的方式是拿一張明知有 alpha 的圖去對答案，並用 `xxd -l 64` 看實際位元組。
現在 `TestInspectDetectsAlpha` 把三種容器的 alpha 判定都釘住了。

另外 RIFF 的 chunk 長度是奇數時後面要補一個 pad byte，漏掉的話後續 chunk
全部錯位 —— 同樣是靜默的。

## 旗標預設值不能取自設定檔

cobra 的 `init()` 在 `main()` 之前執行，那時 `config.Default()` 還沒跑，
viper 是空的。把 `f.IntVarP(&q, "quality", "q", viper.GetInt("QUALITY"), ...)`
寫進 `init()` 會得到 `0`。

正解是旗標預設值留零值，在 `RunE` 裡用 `cmd.Flags().Changed("quality")`
判斷使用者有沒有明確給值，沒給才回頭讀 viper。
