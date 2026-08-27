# img — 圖片格式轉換與檢視 CLI

`img` 在 `webp` / `jpg` / `png` 之間轉換影像並調整尺寸，或只讀檔頭回報影像屬性。
單一執行檔，`不需要安裝 ImageMagick、libwebp 或 cwebp`。

## 為什麼存在 (Why)

日常要處理圖片時，手上的選擇不是 `sips`（macOS 專屬、輸出難解析）就是
ImageMagick / `cwebp`（額外的系統相依）。而 AI agent 要判讀一張圖的尺寸與格式時，
更需要的是穩定的 JSON，而不是給人看的文字。

`img` 把這兩件事收成兩個子命令，輸出同時照顧人與機器。

## 支援的格式 (Formats)

| 格式 | 讀 | 寫 | 說明 |
| :--- | :--: | :--: | :--- |
| `jpg` / `jpeg` | ✅ | ✅ | |
| `png` | ✅ | ✅ | APNG 只讀得出張數，讀不出畫格 |
| `webp` | ✅ | ✅ | lossy / lossless / 動畫皆可 |
| `gif` | ✅ | — | 含動畫；轉成 webp 時整段動畫都會保留 |
| `tiff` / `tif` | ✅ | — | |
| `heic` / `heif` | 認得出格式 | — | 沒有夠成熟的純 Go 解碼器 |
| `avif` | 認得出格式 | — | 同上 |

`heic` 與 `avif` 讀不開，但 `analyze` 仍會`講出它是什麼格式`，而不是丟一句
「不是支援的影像」—— 對著一整個 iPhone 相簿時，這個差別很實際。

## 兩個子命令 (Commands)

### `img analyze` — 這是什麼圖

只解析檔頭，`不解碼像素`，因此對大圖也是瞬間完成。

```bash
img analyze photo.jpg
img analyze ./pics/*.png --json
```

回報：格式、寬 x 高、總像素數、長寬比、color model、位元深度、透明通道、
EXIF orientation、動畫張數、檔案大小。

解不開的格式（`heic`、`avif`）只回報格式與檔案大小，並把 `decodable`
標成 `false` —— 尺寸欄位的 `0` 因此明確表示「沒讀到」而不是「零像素」。

格式一律由 `magic bytes` 判定，`與副檔名無關`。副檔名與實際內容不符時額外標示
`mismatch` —— 這是「明明叫 `.png` 其實是 webp」這類問題唯一能被發現的地方。

### `img convert` — 轉成另一種格式與尺寸

```bash
img convert photo.png -f webp                      # 就地轉檔，輸出到原檔旁
img convert ./pics -f webp -w 1200 -o out/         # 整個目錄批次縮圖
img convert a.png b.png -f jpg -q 90 -o out/       # 多檔輸入
img convert hero.png -f webp --lossless -o hero.webp
img convert photo.heic -f jpg                      # ❌ heic 讀不開，會明確報錯
```

輸出格式限 `jpg` / `png` / `webp`；`gif` 與 `tiff` 只能當來源。

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

## 動畫 (Animation)

`gif` 與 `webp` 的動畫`整段`轉得過去 —— 只要目標是 `webp`：

```bash
img convert loop.gif -f webp                 # 動畫 GIF 轉成動畫 WebP，通常小很多
img convert loop.gif -f webp -w 480          # 每一格都跟著縮
img convert loop.gif -f webp --first-frame   # 只要第一格，壓成靜態圖
```

`jpg` 與 `png` 裝不下動畫，因此會取第一格並在輸出標示 `1/12 frames`
與一則警告 —— `掉幀不會靜悄悄地發生`。

GIF 的畫格常常只是局部矩形，`img` 會依 disposal 規則先疊成完整畫面再轉，
延遲時間與循環次數也一併保留。APNG 目前只讀得出張數，讀不出畫格。

## EXIF 會跟著走 (Metadata Is Carried Over)

拍攝時間、相機、鏡頭與 GPS 預設會`原封不動`地帶進輸出檔，三種輸出格式
（`jpg` 的 APP1、`png` 的 `eXIf` chunk、`webp` 的 `EXIF` chunk）都支援。

同時會做兩件必要的校正，否則帶過去的中繼資料反而會誤導：

- 自動轉正之後把 `orientation` 歸 `1`，`避免看圖軟體再轉一次`。
- 縮圖之後同步更新 EXIF 的尺寸欄位，並丟掉指向舊畫面的內嵌縮圖。

```bash
img convert photo.jpg -f webp --strip-exif -o web/photo.webp
```

`要發佈到網路上時記得 --strip-exif` —— 手機拍的照片幾乎都帶 GPS 座標。

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
