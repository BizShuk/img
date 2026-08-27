package svc

import "testing"

func TestParseJPEGHeaderReadsOrientation(t *testing.T) {
	img := makeImage(40, 20, true)

	for orientation := 1; orientation <= 8; orientation++ {
		data := jpegWithOrientation(t, img, uint16(orientation))
		got := parseJPEGHeader(data).Orientation
		if got != orientation {
			t.Errorf("orientation = %d, want %d", got, orientation)
		}
	}
}

func TestParseJPEGHeaderWithoutExif(t *testing.T) {
	data := encodeTo(t, makeImage(40, 20, true), FORMAT_JPEG, EncodeOptions{Quality: 80})

	meta := parseJPEGHeader(data)
	if meta.Orientation != ORIENTATION_NONE {
		t.Errorf("orientation = %d, want ORIENTATION_NONE", meta.Orientation)
	}
	if meta.BitDepth != 8 {
		t.Errorf("bit depth = %d, want 8", meta.BitDepth)
	}
}

func TestOrientationFromTIFFRejectsGarbage(t *testing.T) {
	cases := [][]byte{
		nil,
		[]byte("II"),
		[]byte("XX\x2a\x00\x08\x00\x00\x00"),
		[]byte("II\x99\x99\x08\x00\x00\x00"),
	}
	for i, b := range cases {
		if got := orientationFromTIFF(b); got != ORIENTATION_NONE {
			t.Errorf("case %d: orientation = %d, want ORIENTATION_NONE", i, got)
		}
	}
}

// orientation 5-8 會把影像轉九十度，長寬必須跟著互換。
func TestApplyOrientationSwapsDimensions(t *testing.T) {
	src := makeImage(40, 20, true)

	for _, orientation := range []int{5, 6, 7, 8} {
		got := ApplyOrientation(src, orientation).Bounds()
		if got.Dx() != 20 || got.Dy() != 40 {
			t.Errorf("orientation %d gave %dx%d, want 20x40", orientation, got.Dx(), got.Dy())
		}
	}

	for _, orientation := range []int{0, 1, 2, 3, 4} {
		got := ApplyOrientation(src, orientation).Bounds()
		if got.Dx() != 40 || got.Dy() != 20 {
			t.Errorf("orientation %d gave %dx%d, want 40x20", orientation, got.Dx(), got.Dy())
		}
	}
}

// orientation 6 是手機直拍最常見的值：順時針轉 90 度後，原本的左下角
// 應該落在左上角。
func TestApplyOrientationRotatesPixels(t *testing.T) {
	src := makeImage(4, 2, true)
	rotated := ApplyOrientation(src, 6)

	wantR, wantG, wantB, _ := src.At(0, 1).RGBA()
	gotR, gotG, gotB, _ := rotated.At(0, 0).RGBA()
	if gotR != wantR || gotG != wantG || gotB != wantB {
		t.Errorf("rotated(0,0) = %v, want src(0,1) = %v",
			[]uint32{gotR, gotG, gotB}, []uint32{wantR, wantG, wantB})
	}
}

// 自動轉正之後若把原封不動的 EXIF 寫進輸出，看圖軟體會依舊的 orientation
// 再轉一次 —— 使用者看到的是被轉了兩次的圖。這是整段 EXIF 保存最容易
// 出錯、也最難察覺的一點。
func TestRetargetExifNeutralisesOrientationAfterRotation(t *testing.T) {
	block := buildExifBlock(exifFixture{Orientation: 6, Width: 1600, Height: 1200, Make: "TestCam", DateTime: "2026:08:27 10:30:00"})

	rotated := RetargetExif(block, 1200, 1600, true)
	if got := orientationFromTIFF(rotated); got != 1 {
		t.Errorf("orientation after rotation = %d, want 1", got)
	}

	kept := RetargetExif(block, 1600, 1200, false)
	if got := orientationFromTIFF(kept); got != 6 {
		t.Errorf("orientation without rotation = %d, want 6 (unchanged)", got)
	}
}

// 縮圖之後 EXIF 的尺寸欄位若不同步更新，中繼資料就在說謊。
// PixelXDimension / PixelYDimension 住在 Exif sub-IFD 裡，容易被漏掉。
func TestRetargetExifUpdatesDimensionsInBothIFDs(t *testing.T) {
	block := buildExifBlock(exifFixture{Orientation: 1, Width: 1600, Height: 1200, Make: "TestCam", DateTime: "2026:08:27 10:30:00"})

	out := RetargetExif(block, 800, 600, false)

	ifd0, ok := readTIFF(out)
	if !ok {
		t.Fatal("retargeted block is not valid tiff")
	}
	for tag, want := range map[uint16]uint32{TAG_IMAGE_WIDTH: 800, TAG_IMAGE_LENGTH: 600} {
		got, ok := ifd0.uint(out, tag)
		if !ok || got != want {
			t.Errorf("IFD0 tag %#x = %d (ok=%v), want %d", tag, got, ok, want)
		}
	}

	sub, ok := ifd0.uint(out, TAG_EXIF_IFD)
	if !ok {
		t.Fatal("exif sub-ifd pointer missing")
	}
	exifIFD, ok := readIFD(out, ifd0.order, int(sub))
	if !ok {
		t.Fatal("exif sub-ifd unreadable")
	}
	for tag, want := range map[uint16]uint32{TAG_PIXEL_X_DIM: 800, TAG_PIXEL_Y_DIM: 600} {
		got, ok := exifIFD.uint(out, tag)
		if !ok || got != want {
			t.Errorf("Exif IFD tag %#x = %d (ok=%v), want %d", tag, got, ok, want)
		}
	}
}

// 拍攝時間與相機資訊是保存 EXIF 的全部理由，改寫尺寸時不能連帶動到它們。
func TestRetargetExifKeepsCameraFields(t *testing.T) {
	block := buildExifBlock(exifFixture{Orientation: 6, Width: 1600, Height: 1200, Make: "TestCam", DateTime: "2026:08:27 10:30:00"})

	out := RetargetExif(block, 800, 600, true)

	if got := asciiTag(t, out, 0, 0x010F); got != "TestCam" {
		t.Errorf("Make = %q, want TestCam", got)
	}
	if got := asciiTag(t, out, TAG_EXIF_IFD, 0x9003); got != "2026:08:27 10:30:00" {
		t.Errorf("DateTimeOriginal = %q, want the original timestamp", got)
	}
}

// IFD1 只放縮圖，而縮圖是轉換前的舊畫面。把 next-IFD 指標清零就丟掉它。
func TestRetargetExifDropsTheThumbnailIFD(t *testing.T) {
	block := buildExifBlock(exifFixture{Orientation: 1, Width: 100, Height: 100, Make: "TestCam", DateTime: "2026:08:27 10:30:00"})

	out := RetargetExif(block, 50, 50, false)

	ifd0, ok := readTIFF(out)
	if !ok {
		t.Fatal("not valid tiff")
	}
	if next := ifd0.order.Uint32(out[ifd0.nextIFDAt : ifd0.nextIFDAt+4]); next != 0 {
		t.Errorf("next IFD offset = %d, want 0", next)
	}
}

func TestRetargetExifLeavesGarbageAlone(t *testing.T) {
	garbage := []byte("not a tiff block at all")
	if got := RetargetExif(garbage, 10, 10, true); !bytesEqual(got, garbage) {
		t.Error("garbage should pass through unchanged")
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
