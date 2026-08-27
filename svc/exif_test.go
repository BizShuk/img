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
