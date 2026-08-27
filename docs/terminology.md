# 術語表 (Terminology)

本檔是 `img` 領域名詞的單一定義來源。程式碼、文件與 skill 用到這些詞時，
一律以此處的定義為準。

## 格式與容器 (Formats and Containers)

| 詞 | 定義 |
| :--- | :--- |
| `Format` | 本工具支援的三種影像編碼：`jpeg`、`png`、`webp`。字面值與 Go `image` 套件的 registered format name 一致 |
| `magic bytes` | 檔案開頭用來識別格式的位元組序列。本工具`唯一`的格式判準 |
| `mismatch` | 副檔名指涉的格式與 magic bytes 判定的格式不一致 |
| `container` | 包住壓縮影像資料的檔案結構。PNG 是 chunk 串列，WebP 是 RIFF，JPEG 是 marker 串列 |
| `chunk` | 容器內的一段具名資料。PNG 的 `IHDR` / `tRNS` / `acTL`，WebP 的 `VP8X` / `ALPH` / `ANMF` / `EXIF` |
| `marker` | JPEG 的段落標記，形如 `0xFF 0xNN`。`SOF` 帶取樣精度，`APP1` 帶 EXIF，`SOS` 之後是壓縮資料 |

## 影像屬性 (Image Properties)

| 詞 | 定義 |
| :--- | :--- |
| `pixels` | 寬乘高的總像素數，不是檔案大小 |
| `color model` | 像素的色彩表示法：`RGBA`、`NRGBA`、`YCbCr`、`Gray`、`Palette` 等 |
| `bit depth` | 每個色彩通道的位元數。PNG 由 `IHDR` 給，JPEG 由 `SOF` 給，WebP 固定 8 |
| `alpha` | 透明通道。PNG 看色彩型別與 `tRNS`，WebP 看 `VP8X` 旗標或 `VP8L` 的 alpha 位元，JPEG 永遠沒有 |
| `orientation` | EXIF tag `0x0112`，值 1-8。1 為正立，5-8 會交換長寬。`0` 表示檔案未帶此資訊 |
| `frames` | 動畫張數。靜態圖為 `1`；WebP 數 `ANMF` chunk，APNG 讀 `acTL` |
| `progressive` | JPEG 的漸進式編碼（`SOF2` / `SOF6` / `SOF10` / `SOF14`），與 baseline 相對 |

## 尺寸調整 (Resize)

| 詞 | 定義 |
| :--- | :--- |
| `fit` | 等比縮放到完全落在目標框內。輸出至多等於框，比例不變 |
| `cover` | 等比縮放到填滿目標框，超出的部分置中裁掉。輸出正好等於框，比例不變 |
| `exact` | 直接拉伸到指定尺寸。輸出正好等於框，`允許變形` |
| `scale` | 按倍率縮放，與 `--width` / `--height` 互斥 |
| `upscale` | 把小於目標的來源放大。`預設不做`，需明確加旗標 |
| `flatten` | 把帶 alpha 的影像疊到不透明底色上。轉 JPEG 時必做 |

## 編碼參數 (Encoding)

| 詞 | 定義 |
| :--- | :--- |
| `quality` | 1-100 的有損壓縮品質。對 jpg 與 lossy webp 生效，PNG 無效 |
| `lossless` | WebP 的無損模式，忽略 `quality`。僅 WebP 有此選項 |
| `method` | WebP 的壓縮耗時／體積取捨，0-6。數字越大越慢、檔案越小 |
| `dynamic` | `gen2brain/webp` 經 `purego` dlopen 系統 libwebp 的快路徑。以 `nodynamic` build tag 關閉後走內建純 Go 實作 |
