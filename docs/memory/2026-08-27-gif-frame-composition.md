# 2026-08-27 — GIF 的畫格是碎片，不是畫面

## 背景 (Context)

`convert` 原本走 `image.Decode`，動畫來源只會拿到第一格。補上動畫支援時，
最直覺的做法是「`gif.DecodeAll` 拿到畫格陣列，逐格丟給 `webp.EncodeAll`」——
`這樣做會產出一堆碎片`。

## 陷阱：畫格通常只是變動的矩形

GIF 為了省空間，每一格通常`只重畫變動的區域`。`gif.DecodeAll` 回傳的
`*image.Paletted` 帶的就是那個局部矩形的 bounds，不是整張畫布。

用 Pillow 檢查一段 PIL 產生的「移動方塊」動畫：

```text
frame 0 bbox: (0, 0, 200, 120)     <- 完整畫布
frame 1 bbox: (0, 40, 59, 81)      <- 只有變動的一小條
frame 2 bbox: (30, 40, 89, 81)
```

第 2 格丟給編碼器就是一張 59x41 的紅方塊，背景整個不見。而且
`webp.EncodeAll` 要求所有畫格同尺寸，尺寸不一還會直接失敗。

## 正解：維護一張畫布，逐格疊上去

```text
canvas := 透明的完整畫布
for 每一格:
    若 disposal == DisposalPrevious: 先備份 canvas
    draw.Over(canvas, frame.Bounds(), frame)
    輸出 canvas 的快照        <- 這才是這一格的畫面
    switch disposal:
      DisposalBackground: 把「這一格的矩形範圍」清成透明
      DisposalPrevious:   canvas 還原成備份
      DisposalNone:       什麼都不做
```

`DisposalBackground 清的是這一格的範圍，不是整張畫布`——這點寫錯的話，
只有在多格重疊時才看得出來。

## 延遲單位不同，差十倍

| 格式 | 單位 |
| :--- | :--- |
| GIF `Delay` | 百分之一秒 |
| WebP `Delay` | 毫秒 |

漏掉 `* 10` 的症狀是動畫快到看不清楚，但檔案完全合法、張數也對。

另外 GIF 用 `0` 或 `1` 表示「盡快播放」。照抄同樣會得到超快動畫；
各家瀏覽器一致的做法是改用 100ms，`img` 照做。

## 驗證方式：用 lossless 對答案

`合成錯誤在有損模式下會被壓縮誤差蓋掉`。正確的驗證是用 `--lossless` 轉一遍，
再用獨立讀取器逐格比對：

```bash
img convert move.gif -f webp --lossless -o ll.webp
python3 -c "
from PIL import Image, ImageChops
g=Image.open('move.gif'); w=Image.open('ll.webp')
for i in range(g.n_frames):
    g.seek(i); w.seek(i)
    d=ImageChops.difference(g.convert('RGB'), w.convert('RGB'))
    assert not any(d.getextrema()[c][1] for c in range(3)), i
print('identical')
"
```

`逐格逐點相同`才算過。單元測試 `TestDecodeGIFAnimationHonoursDisposal`
用一段刻意設計的三格 GIF 把三種 disposal 的結果各釘一個像素。

## 掉幀不能靜悄悄

`jpg` 與 `png` 裝不下動畫，APNG 又讀不出畫格。這些情況一律只輸出第一格，
因此 `Result` 同時帶 `Frames` 與 `SourceFrames`，CLI 印成 `1/12 frames`
並發一則 `slog.Warn` 指出原因。使用者自己下 `--first-frame` 時不警告 ——
那是預期中的結果。
