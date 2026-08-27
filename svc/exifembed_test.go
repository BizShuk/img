package svc

import (
	"bytes"
	"testing"
)

func sampleExif() []byte {
	return buildExifBlock(exifFixture{
		Orientation: 6,
		Width:       1600,
		Height:      1200,
		Make:        "TestCam",
		DateTime:    "2026:08:27 10:30:00",
	})
}

// EXIF 要能寫進三種輸出容器，而且再讀回來時內容完全一致 —— 區塊本身
// 是原封不動的 TIFF，只有外層包裝不同。
func TestEmbedExifRoundTripsThroughEveryContainer(t *testing.T) {
	img := makeImage(64, 48, true)
	block := sampleExif()

	for _, f := range EncodableFormats {
		t.Run(string(f), func(t *testing.T) {
			encoded := encodeTo(t, img, f, EncodeOptions{Quality: 80})

			withExif, err := EmbedExif(encoded, f, block, 64, 48)
			if err != nil {
				t.Fatalf("EmbedExif: %v", err)
			}

			// 帶了 EXIF 之後仍然要是一張讀得開的圖。
			decoded, gotFormat, err := Decode(withExif)
			if err != nil {
				t.Fatalf("decode after embedding: %v", err)
			}
			if gotFormat != f {
				t.Errorf("format = %q, want %q", gotFormat, f)
			}
			if b := decoded.Bounds(); b.Dx() != 64 || b.Dy() != 48 {
				t.Errorf("size = %dx%d, want 64x48", b.Dx(), b.Dy())
			}

			got := parseHeader(f, withExif).Exif
			if !bytes.Equal(got, block) {
				t.Errorf("exif read back differs: got %d bytes, want %d", len(got), len(block))
			}
			if asciiTag(t, got, 0, 0x010F) != "TestCam" {
				t.Error("camera make did not survive the round trip")
			}
			if asciiTag(t, got, TAG_EXIF_IFD, 0x9003) != "2026:08:27 10:30:00" {
				t.Error("capture time did not survive the round trip")
			}
		})
	}
}

// 空區塊不該動到輸出。
func TestEmbedExifIsANoOpWithoutABlock(t *testing.T) {
	encoded := encodeTo(t, makeImage(8, 8, true), FORMAT_PNG, EncodeOptions{})

	got, err := EmbedExif(encoded, FORMAT_PNG, nil, 8, 8)
	if err != nil {
		t.Fatalf("EmbedExif: %v", err)
	}
	if !bytes.Equal(got, encoded) {
		t.Error("output changed even though no exif was supplied")
	}
}

// 簡易的單張 WebP 沒有 VP8X，掛 EXIF 前必須先補一個延伸檔頭，
// 並把 EXIF 旗標設起來；原本就有的透明旗標不能被洗掉。
func TestEmbedExifWebPCreatesExtendedHeader(t *testing.T) {
	cases := []struct {
		name      string
		opt       EncodeOptions
		opaque    bool
		wantAlpha bool
	}{
		{"simple lossy", EncodeOptions{Quality: 80}, true, false},
		{"lossless with alpha", EncodeOptions{Lossless: true}, false, true},
		{"lossy with alpha", EncodeOptions{Quality: 80}, false, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			encoded := encodeTo(t, makeImage(64, 48, tc.opaque), FORMAT_WEBP, tc.opt)

			withExif, err := EmbedExif(encoded, FORMAT_WEBP, sampleExif(), 64, 48)
			if err != nil {
				t.Fatalf("EmbedExif: %v", err)
			}

			chunks := riffChunks(withExif)
			if len(chunks) == 0 || chunks[0].Name != "VP8X" {
				t.Fatalf("first chunk = %v, want VP8X", chunks)
			}
			flags := chunks[0].Payload[0]
			if flags&WEBP_FLAG_EXIF == 0 {
				t.Error("EXIF flag not set in VP8X")
			}
			if got := flags&WEBP_FLAG_ALPHA != 0; got != tc.wantAlpha {
				t.Errorf("alpha flag = %v, want %v", got, tc.wantAlpha)
			}
			if parseWebPHeader(withExif).HasAlpha != tc.wantAlpha {
				t.Errorf("has_alpha after embedding = %v, want %v", !tc.wantAlpha, tc.wantAlpha)
			}
		})
	}
}

// 重複掛 EXIF 不該留下兩份互相矛盾的中繼資料。
func TestEmbedExifWebPReplacesAnExistingBlock(t *testing.T) {
	encoded := encodeTo(t, makeImage(32, 32, true), FORMAT_WEBP, EncodeOptions{Quality: 80})

	once, err := EmbedExif(encoded, FORMAT_WEBP, sampleExif(), 32, 32)
	if err != nil {
		t.Fatal(err)
	}
	twice, err := EmbedExif(once, FORMAT_WEBP, sampleExif(), 32, 32)
	if err != nil {
		t.Fatal(err)
	}

	count := 0
	for _, c := range riffChunks(twice) {
		if c.Name == "EXIF" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("got %d EXIF chunks, want 1", count)
	}
}

// JPEG 的段落長度欄位只有 16 位元，超過就寫不進去。這種情況要明確報錯，
// 讓呼叫端決定要不要放棄 EXIF，而不是產出一個壞掉的檔案。
func TestEmbedExifJPEGRejectsOversizedBlocks(t *testing.T) {
	encoded := encodeTo(t, makeImage(16, 16, true), FORMAT_JPEG, EncodeOptions{Quality: 80})

	got, err := EmbedExif(encoded, FORMAT_JPEG, make([]byte, JPEG_SEGMENT_MAX), 16, 16)
	if err == nil {
		t.Fatal("expected an error for an oversized exif block")
	}
	if !bytes.Equal(got, encoded) {
		t.Error("output should be left untouched when embedding fails")
	}
}
