---
name: img
description: Use when converting images between webp, jpg and png, resizing or batch-shrinking images, stripping or preserving photo metadata, or inspecting an image's real format, dimensions, pixel count, colour model, alpha channel or EXIF orientation with the `img` CLI. Also identifies HEIC and AVIF files by magic bytes. Triggers on "convert this to webp", "shrink these images", "what size is this image", "strip GPS from these photos", "把圖轉成 webp", "批次縮圖", "這張圖多大", "清掉照片的定位資訊", or when an image's extension is suspected to be lying about its real format.
---

# img

## Overview

`img` 是單一執行檔的圖片工具，兩個子命令：`analyze` 只讀檔頭回報影像屬性，
`convert` 轉換格式並調整尺寸。

`格式能力分三級`，回答使用者之前先確認目標落在哪一級：

| 格式 | 讀 | 寫 |
| :--- | :--: | :--: |
| `jpg` / `png` / `webp` | ✅ | ✅ |
| `gif` / `tiff` | ✅ | ❌ 只能當來源 |
| `heic` / `avif` | ❌ 只認得出格式名 | ❌ |

使用者拿 HEIC 來要求轉檔時，`img` 幫不上忙 —— 直接說明並改建議 macOS 內建的
`sips -s format jpeg <in> --out <out>`，`不要`假裝可以。

`不需要 ImageMagick、cwebp 或 sips`。要判讀圖片屬性時優先用 `img analyze --json`，
它的輸出是穩定的結構化資料；`sips -g` 的輸出格式隨 macOS 版本變動，`file` 只給一行字串。

## Precondition

```bash
command -v img || (cd ~/projects/tools/img && go install .)
```

`img` 不存在且找不到 source checkout 時，直接回報缺少工具，`不要`改用
ImageMagick 或 sips 代打 —— 那會讓後續步驟的假設（格式判定、EXIF 行為）失效。

## `img analyze` — 判讀圖片

```bash
img analyze <path>... [--json]
```

`一律加 --json`，除非使用者明確要求給人看的輸出。

回傳一個陣列，每個元素的欄位：

| 欄位 | 型別 | 意義 |
| :--- | :--- | :--- |
| `path` | string | 輸入路徑 |
| `format` | string | `jpeg` / `png` / `webp` / `gif` / `tiff` / `heic` / `avif`，`由 magic bytes 判定` |
| `mime` | string | media type |
| `decodable` | bool | `false` 時只有 `format` / `mime` / `bytes` 有意義，尺寸欄位一律為 `0` |
| `ext_format` | string | 副檔名指涉的格式；副檔名無法辨識時不出現 |
| `mismatch` | bool | 副檔名與實際格式不符 |
| `width` / `height` | int | 像素尺寸 |
| `pixels` | int | `width * height` |
| `ratio` | string | 約分後的長寬比，例如 `4:3`；約不出小整數時給 `0.75:1` |
| `color_model` | string | `RGBA` / `NRGBA` / `YCbCr` / `Gray` / `Palette` 等 |
| `bit_depth` | int | 每通道位元數 |
| `has_alpha` | bool | 是否帶透明通道 |
| `orientation` | int | EXIF orientation 1-8；`0` 表示未帶此資訊 |
| `frames` | int | 動畫張數；靜態圖為 `1` |
| `progressive` | bool | 僅 JPEG，非漸進式時不出現 |
| `bytes` | int | 檔案大小 |

### 要點 (What to rely on)

- `format` 與副檔名無關。判斷「這是什麼圖」時只看 `format`，不要看路徑。
- `mismatch` 為 `true` 是實質問題，`要主動告知使用者`：檔案叫 `.png`
  但內容是 webp，很多下游工具（瀏覽器以外）會拒收。
- `orientation` 為 `6` 或 `8` 表示這是`手機直拍且尚未轉正`的照片，
  它的 `width` / `height` 是`儲存時`的尺寸，`顯示時會互換`。
- `decodable` 為 `false` 時，`width` / `height` / `pixels` 的 `0` 表示`沒讀到`
  而不是`零像素`。回報時不要把它講成「這張圖是 0x0」。
- `analyze` `不解碼像素`，因此不會回報主色、平均亮度或畫面內容。
  使用者問「這張圖裡有什麼」時，`img` 幫不上忙，改用視覺模型讀圖。

## `img convert` — 轉換與縮圖

```bash
img convert <path>... -f <webp|jpg|png> [flags]
```

| 旗標 | 作用 |
| :--- | :--- |
| `-f, --format` | 目標格式，`必填`，只收 `jpg` / `png` / `webp` |
| `-o, --output` | 目錄則逐檔輸出並沿用原檔名；檔名則`只允許單一輸入`；未給則輸出到原檔旁 |
| `-w, --width` | 目標寬度 |
| `-H, --height` | 目標高度（`大寫 H`，`-h` 是 help） |
| `--scale` | 倍率縮放，`不能與 -w / -H 併用` |
| `--mode` | `fit`（預設）/ `cover` / `exact`，只在寬高都給時有差別 |
| `--upscale` | 允許放大；`預設不放大` |
| `-q, --quality` | 1-100，對 jpg 與 lossy webp 生效 |
| `--lossless` | webp 專用，`用在 jpg / png 會直接報錯` |
| `--method` | webp 壓縮強度 0-6，越大越慢越小 |
| `--no-auto-rotate` | 保留儲存時的像素方向，不套用 EXIF 轉正 |
| `--strip-exif` | 清掉拍攝時間、相機與 `GPS`；`預設是保留` |
| `--overwrite` | 允許覆寫；`預設拒絕` |
| `--json` | 輸出結構化結果 |

輸入接受多個路徑，也接受目錄（`只掃該層，不遞迴`）。

### 尺寸語意 (Resize semantics)

只給一邊時依原比例算另一邊。兩邊都給時：

- `fit` 等比縮到`框內`，輸出`可能小於`框。
- `cover` 等比縮到`填滿`框後置中裁切，輸出`正好等於`框，`會裁掉內容`。
- `exact` 直接拉伸，輸出`正好等於`框，`會變形`。

需要固定尺寸的縮圖（頭像、卡片）用 `cover`；只是要「不超過某個大小」用 `fit`。
`exact` 只在使用者明確接受變形時使用。

## Common tasks

| 目標 | 指令 |
| :--- | :--- |
| 單檔轉 webp | `img convert hero.png -f webp` |
| 整個目錄批次縮到寬 1200 的 webp | `img convert ./pics -f webp -w 1200 -o out/` |
| 產生 400x400 的正方形縮圖 | `img convert a.jpg -f webp -w 400 -H 400 --mode cover -o thumb.webp` |
| 需要無損（圖表、截圖、線稿） | `img convert chart.png -f webp --lossless -o chart.webp` |
| 檢查一批圖的實際格式 | `img analyze ./pics/*.png --json` |
| 確認照片是否需要轉正 | `img analyze photo.jpg --json`，看 `orientation` |
| 發佈到網路前清掉定位資訊 | `img convert photo.jpg -f webp --strip-exif -o web/` |

## Behaviour to state up front

回報結果給使用者時，這兩個預設行為`會改變輸出`，值得主動說明：

- `EXIF 自動轉正`：帶 orientation 的照片轉檔後長寬可能互換，
  這是`正確`的（顯示結果一致），但數字看起來會與原檔不同。
  要保留原始像素方向得加 `--no-auto-rotate`。
- `轉 JPEG 時壓白底`：JPEG 沒有透明通道，`img` 會把透明區域壓在白底上。
  來源有 alpha 又需要保留透明時，目標格式必須是 `webp` 或 `png`。
- `EXIF 預設跟著走`：拍攝時間、相機、鏡頭與 `GPS 座標`都會帶進輸出檔
  （orientation 與尺寸欄位會自動校正）。輸出行尾的 `exif kept` 就是在講這件事。

## Guardrails

- `預設不覆寫`。批次轉換若中途報 `already exists`，先確認使用者要不要
  `--overwrite`，`不要`自作主張加上去。
- `-o` 指向單一檔名卻給了多個輸入會直接報錯，這是刻意的。批次一律給目錄。
- 轉換是`逐檔獨立`的：單一檔案失敗不會中止其餘檔案，錯誤走 stderr。
  回報時要把成功與失敗的數量分開講清楚。
- `圖片要公開時主動提醒 GPS`。使用者說「要放上網站 / 傳給別人 / 發到社群」時，
  預設加上 `--strip-exif`，並在回報中說明清掉了什麼。反過來，
  使用者在`歸檔`照片時`不要`加 —— 那正是他們要保留的東西。
- `不要`用 `img` 做內容分析（主色、OCR、物件辨識），它只讀檔頭與像素幾何。
- 動畫 WebP 目前`只會轉出第一張`；`analyze` 的 `frames > 1` 時要先告知使用者
  再決定是否轉換。
