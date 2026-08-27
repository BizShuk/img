package svc

import (
	"os"
	"path/filepath"
	"testing"
)

// bigExif 造一個帶巨大 MakerNote 的 EXIF 區塊，模擬真實相機把區塊撐爆的情況。
func bigExif(makerNoteSize int) []byte {
	note := make([]byte, makerNoteSize)
	for i := range note {
		note[i] = byte(i % 251)
	}
	return buildExifBlock(exifFixture{
		Orientation: 6,
		Width:       4000,
		Height:      3000,
		Make:        "TestCam",
		DateTime:    "2026:08:27 10:30:00",
		MakerNote:   note,
		GPS:         true,
	})
}

// 重建之後區塊要小很多，而使用者真正在乎的欄位要一個不少。
func TestCompactExifKeepsWhatMattersAndDropsTheBulk(t *testing.T) {
	block := bigExif(200_000)
	compact := CompactExif(block)

	if len(compact) >= len(block) {
		t.Fatalf("compact block is %d bytes, source was %d; it should be much smaller", len(compact), len(block))
	}
	if len(compact) > 1024 {
		t.Errorf("compact block is %d bytes; the maker note should be gone", len(compact))
	}

	if got := asciiTag(t, compact, 0, 0x010F); got != "TestCam" {
		t.Errorf("Make = %q, want TestCam", got)
	}
	if got := asciiTag(t, compact, TAG_EXIF_IFD, 0x9003); got != "2026:08:27 10:30:00" {
		t.Errorf("DateTimeOriginal = %q, want the original timestamp", got)
	}

	ifd0, ok := readTIFF(compact)
	if !ok {
		t.Fatal("compact block is not valid tiff")
	}
	if v, ok := ifd0.uint(compact, TAG_ORIENTATION); !ok || v != 6 {
		t.Errorf("Orientation = %d (ok=%v), want 6", v, ok)
	}
	for tag, want := range map[uint16]uint32{TAG_IMAGE_WIDTH: 4000, TAG_IMAGE_LENGTH: 3000} {
		if v, ok := ifd0.uint(compact, tag); !ok || v != want {
			t.Errorf("tag %#x = %d (ok=%v), want %d", tag, v, ok, want)
		}
	}
}

// MakerNote 的內部偏移量是相對於原 TIFF header 的，搬家就失效，
// 因此重建時`必須`丟掉 —— 這也是重建不能當預設路徑的原因。
func TestCompactExifDropsTheMakerNote(t *testing.T) {
	compact := CompactExif(bigExif(100_000))

	ifd0, ok := readTIFF(compact)
	if !ok {
		t.Fatal("not valid tiff")
	}
	sub, ok := ifd0.uint(compact, TAG_EXIF_IFD)
	if !ok {
		t.Fatal("exif sub-ifd pointer missing")
	}
	exifIFD, ok := readIFD(compact, ifd0.order, int(sub))
	if !ok {
		t.Fatal("exif sub-ifd unreadable")
	}
	if _, present := exifIFD.fields[0x927C]; present {
		t.Error("maker note survived; the rebuilt block cannot carry it correctly")
	}
}

// GPS 目錄整組保留 —— 只留一半的座標沒有意義。
func TestCompactExifKeepsTheGPSDirectory(t *testing.T) {
	compact := CompactExif(bigExif(100_000))

	ifd0, ok := readTIFF(compact)
	if !ok {
		t.Fatal("not valid tiff")
	}
	at, ok := ifd0.uint(compact, TAG_GPS_IFD)
	if !ok {
		t.Fatal("gps pointer missing")
	}
	gps, ok := readIFD(compact, ifd0.order, int(at))
	if !ok {
		t.Fatal("gps sub-ifd unreadable")
	}
	if len(gps.fields) != 2 {
		t.Errorf("gps directory has %d fields, want 2", len(gps.fields))
	}
}

// 縮不小時原樣回傳，讓呼叫端知道換了也沒用。
func TestCompactExifLeavesSmallBlocksAlone(t *testing.T) {
	block := buildExifBlock(exifFixture{Orientation: 1, Width: 10, Height: 10, Make: "TestCam", DateTime: "2026:08:27 10:30:00"})

	if got := CompactExif(block); len(got) != len(block) {
		t.Errorf("small block was rebuilt into %d bytes from %d; it should be left alone", len(got), len(block))
	}
	if got := CompactExif([]byte("not a tiff")); string(got) != "not a tiff" {
		t.Error("garbage should pass through unchanged")
	}
}

// 重建後的區塊要能寫進三種容器並讀回來。
func TestCompactExifOutputEmbedsCleanly(t *testing.T) {
	compact := CompactExif(bigExif(100_000))
	img := makeImage(32, 24, true)

	for _, f := range EncodableFormats {
		t.Run(string(f), func(t *testing.T) {
			encoded := encodeTo(t, img, f, EncodeOptions{Quality: 80})
			out, err := EmbedExif(encoded, f, compact, 32, 24)
			if err != nil {
				t.Fatalf("EmbedExif: %v", err)
			}
			if got := asciiTag(t, parseHeader(f, out).Exif, 0, 0x010F); got != "TestCam" {
				t.Errorf("Make = %q after embedding, want TestCam", got)
			}
		})
	}
}

// 這是整條路徑的重點：EXIF 大到塞不進 JPEG 段落時，轉檔仍要留下
// 拍攝時間與相機資訊，而不是整段丟掉。
func TestConvertReducesOversizedExifInsteadOfDroppingIt(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "big.jpg")
	// JPEG 的 APP1 上限是 65533，因此來源必須用 png 之類沒有上限的容器裝。
	if err := os.WriteFile(src, pngWithExif(t, makeImage(64, 48, true), bigExif(200_000)), 0o644); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(dir, "out.jpg")
	res, err := ConvertFile(src, dest, defaultRequest(FORMAT_JPEG))
	if err != nil {
		t.Fatalf("ConvertFile: %v", err)
	}
	if !res.Exif {
		t.Fatal("exif should have been carried over in reduced form")
	}
	if !res.ExifReduced {
		t.Error("result should report that the block was reduced")
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	got := parseHeader(FORMAT_JPEG, data).Exif
	if len(got) == 0 {
		t.Fatal("no exif in the output")
	}
	if asciiTag(t, got, 0, 0x010F) != "TestCam" {
		t.Error("camera make lost even though the block was reduced")
	}
	if asciiTag(t, got, TAG_EXIF_IFD, 0x9003) != "2026:08:27 10:30:00" {
		t.Error("capture time lost even though the block was reduced")
	}
	// 轉正過，orientation 必須歸 1。
	if info, err := Inspect(dest); err != nil {
		t.Fatal(err)
	} else if info.Orientation > 1 {
		t.Errorf("orientation = %d after reduction, want 1", info.Orientation)
	}
}

// 容器裝得下的時候不該多此一舉重建。
func TestConvertKeepsTheFullBlockWhenItFits(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "small.jpg")
	block := buildExifBlock(exifFixture{Orientation: 1, Width: 64, Height: 48, Make: "TestCam", DateTime: "2026:08:27 10:30:00", MakerNote: make([]byte, 512)})
	if err := os.WriteFile(src, jpegWithExif(t, makeImage(64, 48, true), block), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := ConvertFile(src, filepath.Join(dir, "out.jpg"), defaultRequest(FORMAT_JPEG))
	if err != nil {
		t.Fatalf("ConvertFile: %v", err)
	}
	if res.ExifReduced {
		t.Error("a block that fits should be carried over untouched")
	}
}
