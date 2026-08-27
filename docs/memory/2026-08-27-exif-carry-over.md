# 2026-08-27 — 轉檔保存 EXIF：複製不夠，必須校正

## 背景 (Context)

首版的 `convert` 一律丟掉 EXIF，等於把拍攝時間、相機、鏡頭與 GPS 全部抹掉。
補上保存功能時最先想到的做法是「把來源的 EXIF 區塊照抄到輸出」——
`這樣做會產生比沒有 EXIF 更糟的結果`。

## 三個非做不可的校正

### 1. orientation 必須歸 1（否則轉兩次）

手機直拍的照片帶 `orientation=6`，像素其實是橫的，靠這個 tag 讓看圖軟體轉正。
`img` 預設會`實際旋轉像素`。如果同時把 `orientation=6` 原封不動抄過去，
看圖軟體會`再轉一次` —— 使用者拿到一張躺著的圖，而且會怪罪轉檔工具。

`這個 bug 極難自己察覺`：轉出來的檔案在 `img analyze` 眼裡完全正常
（像素是正的），只有用真的看圖軟體開才會發現。驗證方式是拿
Pillow 這類獨立讀取器對答案：

```bash
python3 -c "from PIL import Image; im=Image.open('out.jpg'); print(im.size, im.getexif().get(0x0112))"
```

正確結果是`像素尺寸已交換`且 `orientation == 1`。

### 2. 尺寸欄位要同步，而且有兩處

縮圖之後，EXIF 裡的尺寸還停在原圖。要改的是`兩組`欄位：

- IFD0 的 `ImageWidth` (0x0100) / `ImageLength` (0x0101)
- Exif sub-IFD（由 tag `0x8769` 指向）的 `PixelXDimension` (0xA002) /
  `PixelYDimension` (0xA003)

`第二組住在 sub-IFD 裡，最容易漏掉`。

### 3. IFD1 要丟掉

IFD1 放的是內嵌縮圖，內容是`轉換前的舊畫面`。留著它，某些看圖軟體的
列表檢視會顯示舊圖。把 IFD0 的 next-IFD 指標清零就等於丟掉它。

## 一律就地改寫，絕不重新序列化

TIFF 的 IFD 到處是指向區塊內其他位置的`絕對偏移量`：ASCII 字串、sub-IFD、
RATIONAL 值都是。重新序列化就得把所有偏移量跟著搬，
`那是把一份原本能用的 EXIF 弄壞最快的方法`。

因此 `RetargetExif` 只做兩件事：複製一份、就地覆寫既有欄位的值。
型別不符或值落在區塊外的欄位一律放著不動，不新增、不搬移、不擴充。

## 塞不進 JPEG 時：重建，不是放棄

JPEG 的 APP1 段落上限是 `65533` 位元組。相機的 `MakerNote`（tag `0x927C`）
動輒 100-300 KB，因此`真實照片的 EXIF 常常塞不進 JPEG`。

原本的處理是整段放棄，等於「因為留不住冷門欄位，所以連拍攝時間也不留」。
現在改成 `CompactExif`：重建一份只含必要欄位的區塊再試一次。

`MakerNote 必丟，不是選擇題。` 它的內部偏移量多半相對於原 TIFF header，
搬到新位置就失效 —— 留下來只會是一段壞掉的私有資料。這同時是
`重建不能當成預設路徑`的原因：容器裝得下時，原封不動地帶著它才是對的。

`位元組序要沿用來源。` 欄位的值是照抄的原始位元組；新區塊若改寫成
不同的位元組序，那些數字會被讀成完全不同的值。

`目錄內的欄位必須依 tag 遞增排列`，這是 TIFF 的規格要求，漏掉會讓
部分讀取器解析失敗。

### 驗證：一定要用外部讀取器

自己寫的 TIFF 序列化器不能只用自己寫的解析器驗證。用 Pillow 對答案：

```bash
python3 -c "
from PIL import Image
from PIL.ExifTags import TAGS, GPSTAGS
im=Image.open('out.jpg'); ex=im.getexif()
sub=ex.get_ifd(0x8769); gps=ex.get_ifd(0x8825)
print(ex.get(0x010F), sub.get(0x9003), sub.get(0x829D), len(gps))
"
```

重點是`要看到 RATIONAL 型別（如 FNumber）與 GPS 子目錄`都讀得出來 ——
它們的值都存在資料區，是重新指派偏移量最容易出錯的地方。

## 為什麼不用 EXIF 函式庫

`dsoprea/go-exif` 體積大且 API 難用，`rwcarlsen/goexif` 已封存。
而本工具需要的操作只有「照抄整塊 + 改四個整數欄位 + 清一個指標」，
自寫一個小型 IFD 讀寫器比適配任何一個函式庫都簡單，也比較不會出錯。

## 三種容器的擺放位置不同

EXIF 區塊本身在三種容器裡`完全相同`（原始 TIFF），差別只在外層包裝：

| 容器 | 位置 | 陷阱 |
| :--- | :--- | :--- |
| JPEG | `APP1` 段落，SOI 與 APP0 之後 | 段落長度只有 16 位元，`超過 65533 就寫不進去` |
| PNG | `eXIf` chunk，插在第一個 `IDAT` 之前 | 插在 IEND 前也合法，但讀取端得掃完整份影像資料才拿得到 |
| WebP | `EXIF` chunk | 簡易單張檔`沒有 VP8X`，得先補一個延伸檔頭才有地方放旗標，且補的時候不能洗掉透明旗標 |

WebP 那條踩過一次：補 VP8X 時只設了 `EXIF` 旗標，把原本的 `ALPHA` 旗標弄丟，
結果帶透明的圖轉完之後 `analyze` 回報「沒有透明通道」。

## 隱私

`GPS 座標也在 EXIF 裡`，且預設會被帶過去。`--strip-exif` 是發佈用的開關，
README 與 SKILL 都明講了。SKILL 進一步要求：使用者提到「放上網站 / 傳給別人」
時主動加上這個旗標。
