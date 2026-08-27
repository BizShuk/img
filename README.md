# img — 圖片格式轉換與檢視 CLI

`img` 在 `webp` / `jpg` / `png` 之間轉換影像並調整尺寸，或只讀檔頭回報影像屬性。
單一執行檔，`不需要安裝 ImageMagick、libwebp 或 cwebp`。

## 為什麼存在 (Why)

日常要處理圖片時，手上的選擇不是 `sips`（macOS 專屬、輸出難解析）就是
ImageMagick / `cwebp`（額外的系統相依）。而 AI agent 要判讀一張圖的尺寸與格式時，
更需要的是穩定的 JSON，而不是給人看的文字。

`img` 把這兩件事收成兩個子命令，輸出同時照顧人與機器。

## 兩個子命令 (Commands)

### `img analyze` — 這是什麼圖

只解析檔頭，`不解碼像素`，因此對大圖也是瞬間完成。

```bash
img analyze photo.jpg
img analyze ./pics/*.png --json
```

回報：格式、寬 x 高、總像素數、長寬比、color model、位元深度、透明通道、
EXIF orientation、動畫張數、檔案大小。

格式一律由 `magic bytes` 判定，`與副檔名無關`。副檔名與實際內容不符時額外標示
`mismatch` —— 這是「明明叫 `.png` 其實是 webp」這類問題唯一能被發現的地方。

### `img convert` — 轉成另一種格式與尺寸

```bash
img convert photo.png -f webp                      # 就地轉檔，輸出到原檔旁
img convert ./pics -f webp -w 1200 -o out/         # 整個目錄批次縮圖
img convert a.png b.png -f jpg -q 90 -o out/       # 多檔輸入
img convert hero.png -f webp --lossless -o hero.webp
```

輸入接受多個檔案路徑，也接受目錄（`只掃該層，不遞迴`）。
`-o` 指向目錄時逐檔輸出並沿用原檔名；指向檔名時只允許單一輸入；
未給時輸出到原檔旁邊。`預設拒絕覆寫`既有檔案。

## 尺寸調整的三種語意 (Resize Semantics)

只給 `--width` 或 `--height` 時依原比例算出另一邊。兩邊都給時由 `--mode` 決定：

| 模式 | 行為 | 輸出尺寸 |
| :--- | :--- | :--- |
| `fit`（預設） | 等比縮到完全落在框內 | 至多等於框，比例不變 |
| `cover` | 等比縮到填滿框，超出的部分置中裁掉 | 正好等於框，比例不變 |
| `exact` | 直接拉伸到指定尺寸 | 正好等於框，`允許變形` |

`--scale 0.5` 按倍率縮放，不能與 `--width` / `--height` 併用。
`預設一律不放大`；來源比目標小時維持原尺寸，要放大得加 `--upscale`。

## 兩個預設的判斷 (Defaults That Matter)

- `EXIF 自動轉正` —— 手機直拍的照片帶 orientation tag，轉檔時若不套用，
  輸出就會躺著。`img` 預設轉正，`--no-auto-rotate` 可以關掉。
- `轉 JPEG 時壓白底` —— JPEG 沒有透明通道，直接編碼會讓透明區域變成
  不可預期的顏色。`img` 先把影像疊在白底上再編碼。

## 設定 (Configuration)

首次執行會在 `~/.config/img/settings.json` 落下預設值。命令列旗標永遠優先於設定檔。

| 鍵 | 預設 | 作用 |
| :--- | :--- | :--- |
| `QUALITY` | `82` | jpg 與 lossy webp 的品質 |
| `WEBP_METHOD` | `4` | webp 壓縮耗時／體積取捨，0 最快、6 最小 |
| `RESIZE_MODE` | `fit` | 未給 `--mode` 時的預設模式 |
| `LOG_LEVEL` | `warn` | 日誌等級 |

`img config show` 檢視目前生效的設定。

## 安裝 (Installation)

```bash
go install github.com/bizshuk/img@latest
```

技術脈絡、套件結構與關鍵決策見 [CLAUDE.md](CLAUDE.md)。
