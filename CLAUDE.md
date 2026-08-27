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
│   ├── format.go             # Format 列舉、能力判定、ftyp 品牌嗅探
│   ├── codec.go              # Decode / Encode 分派、alpha 壓平
│   ├── meta.go               # 五種容器的檔頭走訪
│   ├── animation.go          # 畫格序列的讀寫與 GIF disposal 合成
│   ├── riff.go               # WebP 的 RIFF chunk 拆解與重組
│   ├── exif.go               # TIFF IFD 讀取、orientation、就地改寫
│   ├── exifembed.go          # 把 EXIF 區塊寫進三種輸出容器
│   ├── exifcompact.go        # 區塊過大時重建一份只含必要欄位的
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
| `golang.org/x/image/tiff` | TIFF 解碼 | 只註冊解碼器；TIFF 不是 convert 的目標格式 |
| `image/gif` (stdlib) | GIF 解碼 | 同上 |
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

### 格式能力用述詞表達，不維護平行清單

`Format` 涵蓋七種格式，能力由三個述詞決定，`不要`再引入第四張清單：

| 述詞 | 成員 | 意義 |
| :--- | :--- | :--- |
| `Encodable()` | jpeg, png, webp | `convert -f` 的合法目標 |
| `Decodable()` | 前三者 + gif, tiff | 可以當轉換來源，`analyze` 給完整報告 |
| `Animatable()` | gif, webp | 畫格序列讀得出來 |
| 皆為 false | heic, avif | 只報得出格式名與檔案大小 |

`Animatable()` 刻意`不含 png`：`analyze` 讀得到 APNG 的 `acTL` 張數，
但標準庫的解碼器只給第一張。把 png 列進去會讓 convert 以為能保留動畫。

`ParseFormat` 接受全部七種（供副檔名比對用），`ParseTargetFormat` 只接受可寫的。
兩者混用會讓 `-f heic` 一路走到編碼器才炸掉。

`analyze` 對第三級走 `SniffUndecodable`：比對 ISO BMFF 的 `ftyp` box 品牌碼。
品牌可能只出現在`相容清單`而不是 major brand（`mif1` + `avif` 就是 AVIF），
因此兩邊都要掃，且 AVIF 要先於 HEIC 判斷。

### 格式一律由 magic bytes 判定

`image.DecodeConfig` / `image.Decode` 回傳的 registered format name 就是判準。
`Format` 的字面值刻意與這些名稱一致（`jpeg` / `png` / `webp`），可以直接轉型。
副檔名`只`用來計算 `Info.Mismatch`，絕不參與解碼決策。

`唯一的例外`是 `svc.ExpandInputs` 掃描目錄時：那裡用副檔名過濾，
純粹是為了不去開啟目錄下每一個無關檔案。被選中的檔案仍走 magic bytes。

### 自寫 TIFF IFD 讀寫，不引入 EXIF 函式庫

`svc/exif.go` 是一個小型的 TIFF IFD 讀寫器：解析 IFD0 與 sub-IFD、
讀出整數欄位、`就地`改寫既有欄位。`dsoprea/go-exif` 體積大且 API 難用，
`rwcarlsen/goexif` 已封存；而本工具需要的操作只有「照抄整塊 + 改四個欄位」，
自寫比適配任何一個函式庫都簡單。

`關鍵約束：預設一律就地改寫，絕不重新序列化。` TIFF 的 IFD 到處是指向區塊內
其他位置的絕對偏移量（ASCII 字串、sub-IFD、RATIONAL 值）。一旦重新序列化，
所有偏移量都要跟著搬，那是把一份原本能用的 EXIF 弄壞最快的方法。
改不動的欄位（型別不符、值在區塊外）就放著不管。

`唯一的例外`是區塊塞不進容器時的重建，見下節。

### 轉檔時 EXIF 必須被「校正」而不只是「複製」

`svc.RetargetExif` 在複製之外做三件事，少做任何一件，帶過去的中繼資料
都會比沒有更糟：

1. `orientation 歸 1`（僅在確實轉正過時）。否則看圖軟體會依舊值`再轉一次`，
   使用者看到被轉了兩次的圖。這是整段功能最容易出錯也最難察覺的一點。
2. `尺寸欄位同步`：IFD0 的 `ImageWidth` / `ImageLength`，以及 Exif sub-IFD 裡的
   `PixelXDimension` / `PixelYDimension`。sub-IFD 那組很容易被漏掉。
3. `丟掉 IFD1`：那裡放的是內嵌縮圖，內容是轉換前的舊畫面。
   把 IFD0 的 next-IFD 指標清零即可，縮圖位元組成為無人引用的死重。

`隱私：` GPS 座標也在 EXIF 裡，且預設會被帶過去。`--strip-exif` 是發佈用的開關，
README 與 SKILL 都明講了這一點。

### 區塊過大時重建，而不是整段丟掉

`svc/exifcompact.go`。JPEG 的 APP1 段落上限是 65533 位元組，而相機的
`MakerNote` 動輒數百 KB。`embedExifJPEG` 遇到這種情況回傳
`ErrExifTooLarge`，`attachExif` 據此改用 `CompactExif` 重建一份較小的再試 ——
`少幾個冷門欄位遠好過丟掉拍攝時間與相機資訊`。

保留清單（`keepIFD0` / `keepExif`）的取捨標準是「使用者為什麼要留著 EXIF」：
相機與鏡頭、拍攝時間、曝光參數。GPS 目錄整組保留 —— 欄位都很小，
而且只留一半的座標沒有意義。

`MakerNote 必丟，不是選擇題。` 它的內部偏移量多半相對於原 TIFF header，
搬到新位置就失效。這同時也是`重建不能當成預設路徑`的原因：
一般情況下寧可原封不動地帶著它，也不要重建後留下一段壞掉的私有資料。

`位元組序沿用來源。` 欄位的值是照抄的原始位元組，換了位元組序會被讀成
完全不同的數字。同理，目錄內的欄位必須依 tag 遞增排列，這是 TIFF 的規格要求。

`Result.ExifReduced` 讓 CLI 印出 `exif kept (reduced)`，並發一則 `slog.Info`
說明丟了什麼 —— 有損的取捨不能默默發生。

### 三種輸出容器各自的 EXIF 擺放方式

`svc/exifembed.go`。EXIF 區塊本身在三種容器裡完全相同（原始 TIFF），
差別只在外層包裝：

- `JPEG` 插入 `APP1` 段落，位置在 SOI 與既有 APP0 (JFIF) 之後。
  段落長度欄位只有 16 位元，`超過 65533 就寫不進去`，此時明確報錯，
  由呼叫端決定放棄 EXIF 而不是產出壞檔。
- `PNG` 插在`第一個 IDAT 之前`的 `eXIf` chunk。規格允許放在 IDAT 前後，
  放前面讀取端才不必掃完整份影像資料。CRC 涵蓋 type 與 payload。
- `WebP` 要`重組整個 RIFF 容器`。簡易的單張 VP8 / VP8L 檔沒有 VP8X，
  得先補一個延伸檔頭才有地方放 EXIF 旗標，且補的時候不能洗掉透明旗標。
  已經是延伸格式的則補旗標並在尾端接 chunk。既有的 EXIF chunk 一律換掉。

### 五種容器各自的檔頭走訪

`svc/meta.go` 補的是 `image.Config` 給不出來的東西：位元深度、透明通道、
動畫張數、orientation 與原始 EXIF 區塊。各容器的結構完全不同，
`最容易寫反的是 RIFF`：

- `PNG` 是 `length(4) + type(4) + payload`
- `RIFF (WebP)` 是 `FourCC(4) + length(4) + payload`，`順序相反`，
  且奇數長度的 chunk 後面要補一個 pad byte
- `JPEG` 是 marker 串列；`0xD0-0xD9`、`0x01`、`0xFF` 沒有長度欄位，
  `0xC4` / `0xC8` / `0xCC` 落在 SOF 的區間裡但不是 SOF
- `GIF` 是 block 串列，資料以`長度前綴的 sub-block` 串接，
  要正確跳過才數得對張數；透明色旗標在 Graphic Control Extension 的 packed byte
- `TIFF` 檔本身就是一個 TIFF 區塊，因此直接餵給 `readTIFF` 即可。
  `不`把整個檔案當成 EXIF 帶進 convert —— IFD 夾雜著指向影像資料的偏移量，
  整份複製到 JPEG 的 APP1 裡既超長也沒有意義

### GIF 的畫格是局部的，必須先合成

`svc/animation.go`。`gif.DecodeAll` 給的 `*image.Paletted` 常常`只是變動的
小矩形`，直接拿來當畫格會得到一堆碎片。正確做法是維護一張畫布，逐格
`draw.Over` 疊上去、拍下快照，再依 `Disposal` 決定如何還原：

| Disposal | 疊完之後 |
| :--- | :--- |
| `DisposalNone` (0) | 保留，下一格繼續疊在上面 |
| `DisposalBackground` | 把`這一格的矩形範圍`清成透明 |
| `DisposalPrevious` | 整張畫布還原成疊之前的狀態（因此要先備份） |

`延遲單位不同：`GIF 以百分之一秒計，WebP 以毫秒計，換算漏掉就會快十倍。
GIF 用 `0` 或 `1` 表示「盡快播放」，各家瀏覽器一致改用 100ms，`img` 照做。

`驗證方式：`用 `--lossless` 轉一遍，輸出應與來源`逐格逐點相同`。
有損模式下合成錯誤會被壓縮誤差掩蓋，看不出來。

### 動畫的轉換路徑與靜態圖分開

`ConvertRequest.animates()` 決定走哪一條，三個條件缺一不可：來源真的多格、
來源格式 `Animatable()`、目標是 webp。兩條路徑共用同一個 `transform`
閉包（轉正 + 縮放），因此每格都走同樣的處理，輸出保持同尺寸 ——
`webp.EncodeAll` 要求所有畫格同尺寸。

走不到動畫路徑時，`Result.Frames` 與 `Result.SourceFrames` 會不相等，
CLI 印成 `1/12 frames` 並發一則 `slog.Warn` 說明原因。
`掉幀絕不能靜悄悄地發生`——但使用者自己下 `--first-frame` 時不必警告。

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
即時產生，帶 EXIF 的 JPEG 由 `jpegWithOrientation` 手工組出 APP1 段落，
含局部畫格與 disposal 的動畫 GIF 由 `svc/animation_test.go` 的
`partialFrameGIF` 組出。

`buildExifBlock` 是`測試端獨立實作`的 TIFF 組裝器，刻意不共用 production 的
`buildTIFF`：兩邊若共用同一份版面計算，計算本身有錯時測試也看不出來。
新增格式時，`TestConvertRoundTripsEveryFormatPair` 會自動涵蓋所有新的組合。

`cmd/` 的測試要注意：旗標綁在套件層變數上，且 cobra 的子命令一律經由
根命令解析，因此 `cmd_test.go` 的 `run` 每次執行前會把旗標歸零。
`TestMain` 只設 viper 預設值，`不呼叫 config.Default` —— 那會在使用者的
`~/.config/img/` 落下檔案。
