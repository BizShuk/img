package svc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seed 在 dir 底下寫入一張指定格式的測試圖，回傳路徑。
func seed(t *testing.T, dir, name string, f Format, w, h int, opaque bool) string {
	t.Helper()
	path := filepath.Join(dir, name)
	data := encodeTo(t, makeImage(w, h, opaque), f, EncodeOptions{Quality: 80})
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("seed %s: %v", path, err)
	}
	return path
}

func defaultRequest(target Format) ConvertRequest {
	return ConvertRequest{
		Target:       target,
		Resize:       ResizeSpec{Mode: MODE_FIT},
		Encode:       EncodeOptions{Quality: 80},
		AutoRotate:   true,
		PreserveExif: true,
	}
}

// 九種格式組合都要能來回轉換，且尺寸不變。
func TestConvertRoundTripsEveryFormatPair(t *testing.T) {
	for _, from := range EncodableFormats {
		for _, to := range EncodableFormats {
			t.Run(string(from)+"_to_"+string(to), func(t *testing.T) {
				dir := t.TempDir()
				src := seed(t, dir, "src"+from.Ext(), from, 64, 48, true)
				dest := filepath.Join(dir, "out"+to.Ext())

				res, err := ConvertFile(src, dest, defaultRequest(to))
				if err != nil {
					t.Fatalf("ConvertFile: %v", err)
				}
				if res.Width != 64 || res.Height != 48 {
					t.Errorf("size = %dx%d, want 64x48", res.Width, res.Height)
				}

				info, err := Inspect(dest)
				if err != nil {
					t.Fatalf("Inspect: %v", err)
				}
				if info.Format != to {
					t.Errorf("written format = %q, want %q", info.Format, to)
				}
			})
		}
	}
}

func TestConvertResizesWhileChangingFormat(t *testing.T) {
	dir := t.TempDir()
	src := seed(t, dir, "src.png", FORMAT_PNG, 1600, 1200, true)
	dest := filepath.Join(dir, "out.webp")

	req := defaultRequest(FORMAT_WEBP)
	req.Resize = ResizeSpec{Width: 800, Mode: MODE_FIT}

	res, err := ConvertFile(src, dest, req)
	if err != nil {
		t.Fatalf("ConvertFile: %v", err)
	}
	if res.Width != 800 || res.Height != 600 {
		t.Errorf("size = %dx%d, want 800x600", res.Width, res.Height)
	}
}

// 手機直拍的 orientation=6 照片轉檔後應該已經轉正，長寬因此互換。
func TestConvertAppliesExifOrientation(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "rotated.jpg")
	if err := os.WriteFile(src, jpegWithOrientation(t, makeImage(80, 40, true), 6), 0o644); err != nil {
		t.Fatal(err)
	}

	rotated, err := ConvertFile(src, filepath.Join(dir, "rotated.png"), defaultRequest(FORMAT_PNG))
	if err != nil {
		t.Fatalf("ConvertFile: %v", err)
	}
	if rotated.Width != 40 || rotated.Height != 80 {
		t.Errorf("auto-rotated size = %dx%d, want 40x80", rotated.Width, rotated.Height)
	}

	req := defaultRequest(FORMAT_PNG)
	req.AutoRotate = false
	kept, err := ConvertFile(src, filepath.Join(dir, "kept.png"), req)
	if err != nil {
		t.Fatalf("ConvertFile: %v", err)
	}
	if kept.Width != 80 || kept.Height != 40 {
		t.Errorf("--no-auto-rotate size = %dx%d, want 80x40", kept.Width, kept.Height)
	}
}

func TestConvertRefusesToOverwriteUnlessAsked(t *testing.T) {
	dir := t.TempDir()
	src := seed(t, dir, "src.png", FORMAT_PNG, 32, 32, true)
	dest := filepath.Join(dir, "out.webp")

	if _, err := ConvertFile(src, dest, defaultRequest(FORMAT_WEBP)); err != nil {
		t.Fatalf("first convert: %v", err)
	}
	if _, err := ConvertFile(src, dest, defaultRequest(FORMAT_WEBP)); err == nil {
		t.Error("second convert should refuse to overwrite")
	}

	req := defaultRequest(FORMAT_WEBP)
	req.Overwrite = true
	if _, err := ConvertFile(src, dest, req); err != nil {
		t.Errorf("--overwrite should allow replacing: %v", err)
	}
}

// 轉 JPEG 時透明區域必須壓在白底上，否則 image/jpeg 會直接丟掉 alpha，
// 透明處變成不可預期的顏色。
func TestConvertFlattensAlphaOntoWhiteForJPEG(t *testing.T) {
	dir := t.TempDir()
	src := seed(t, dir, "src.png", FORMAT_PNG, 64, 48, false)
	dest := filepath.Join(dir, "out.jpg")

	if _, err := ConvertFile(src, dest, defaultRequest(FORMAT_JPEG)); err != nil {
		t.Fatalf("ConvertFile: %v", err)
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	img, _, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}

	// 左側 1/4 在來源是全透明，壓白之後每個通道都應該接近滿值。
	r, g, b, _ := img.At(2, 24).RGBA()
	if r < 0xF000 || g < 0xF000 || b < 0xF000 {
		t.Errorf("transparent area became %v, want near white", []uint32{r, g, b})
	}
}

func TestExpandInputsScansDirectoriesNonRecursively(t *testing.T) {
	dir := t.TempDir()
	seed(t, dir, "a.png", FORMAT_PNG, 8, 8, true)
	seed(t, dir, "b.webp", FORMAT_WEBP, 8, 8, true)
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(dir, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	seed(t, nested, "c.png", FORMAT_PNG, 8, 8, true)

	files, errs := ExpandInputs([]string{dir})
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(files) != 2 {
		t.Fatalf("got %d files, want 2 (a.png, b.webp): %v", len(files), files)
	}
}

func TestConvertOutputTargetResolution(t *testing.T) {
	dir := t.TempDir()
	a := seed(t, dir, "a.png", FORMAT_PNG, 16, 16, true)
	b := seed(t, dir, "b.png", FORMAT_PNG, 16, 16, true)

	t.Run("directory output keeps basenames", func(t *testing.T) {
		out := filepath.Join(dir, "batch")
		req := defaultRequest(FORMAT_WEBP)
		req.Output = out

		results, errs := Convert([]string{a, b}, req)
		if len(errs) != 0 {
			t.Fatalf("unexpected errors: %v", errs)
		}
		if len(results) != 2 {
			t.Fatalf("got %d results, want 2", len(results))
		}
		for _, name := range []string{"a.webp", "b.webp"} {
			if _, err := os.Stat(filepath.Join(out, name)); err != nil {
				t.Errorf("expected %s: %v", name, err)
			}
		}
	})

	t.Run("file output rejects multiple inputs", func(t *testing.T) {
		req := defaultRequest(FORMAT_WEBP)
		req.Output = filepath.Join(dir, "one.webp")

		if _, errs := Convert([]string{a, b}, req); len(errs) == 0 {
			t.Error("a single-file -o with two inputs should fail")
		}
	})

	t.Run("no output writes alongside the source", func(t *testing.T) {
		results, errs := Convert([]string{a}, defaultRequest(FORMAT_WEBP))
		if len(errs) != 0 {
			t.Fatalf("unexpected errors: %v", errs)
		}
		if want := filepath.Join(dir, "a.webp"); results[0].Dest != want {
			t.Errorf("dest = %q, want %q", results[0].Dest, want)
		}
	})
}

// gif 與 tiff 讀得進來但寫不出去，因此它們只能出現在轉換的來源端。
func TestConvertAcceptsGIFAndTIFFAsSources(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"src.gif", encodeGIF(t, makeImage(64, 48, true))},
		{"src.tif", encodeTIFF(t, makeImage(64, 48, true))},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, tc.name)
			if err := os.WriteFile(src, tc.data, 0o644); err != nil {
				t.Fatal(err)
			}

			res, err := ConvertFile(src, filepath.Join(dir, "out.webp"), defaultRequest(FORMAT_WEBP))
			if err != nil {
				t.Fatalf("ConvertFile: %v", err)
			}
			if res.Width != 64 || res.Height != 48 {
				t.Errorf("size = %dx%d, want 64x48", res.Width, res.Height)
			}
		})
	}
}

// heic 解不開，但錯誤訊息要講出是哪一種格式，而不是一句無從追查的失敗。
func TestConvertNamesUndecodableSources(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "photo.heic")
	if err := os.WriteFile(src, fakeHEIC("heic", "mif1"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ConvertFile(src, filepath.Join(dir, "out.png"), defaultRequest(FORMAT_PNG))
	if err == nil {
		t.Fatal("converting a heic should fail")
	}
	if !strings.Contains(err.Error(), "heic") {
		t.Errorf("error %q should name the format", err)
	}
}

// 拍攝時間與相機資訊要跨格式活下來，這是保存 EXIF 的全部目的。
func TestConvertCarriesExifAcrossFormats(t *testing.T) {
	for _, target := range EncodableFormats {
		t.Run(string(target), func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "src.jpg")
			block := buildExifBlock(exifFixture{Orientation: 1, Width: 80, Height: 40, Make: "TestCam", DateTime: "2026:08:27 10:30:00"})
			if err := os.WriteFile(src, jpegWithExif(t, makeImage(80, 40, true), block), 0o644); err != nil {
				t.Fatal(err)
			}

			dest := filepath.Join(dir, "out"+target.Ext())
			res, err := ConvertFile(src, dest, defaultRequest(target))
			if err != nil {
				t.Fatalf("ConvertFile: %v", err)
			}
			if !res.Exif {
				t.Fatal("result should report that exif was carried over")
			}

			data, err := os.ReadFile(dest)
			if err != nil {
				t.Fatal(err)
			}
			got := parseHeader(target, data).Exif
			if len(got) == 0 {
				t.Fatal("no exif in the output file")
			}
			if asciiTag(t, got, 0, 0x010F) != "TestCam" {
				t.Error("camera make lost in conversion")
			}
			if asciiTag(t, got, TAG_EXIF_IFD, 0x9003) != "2026:08:27 10:30:00" {
				t.Error("capture time lost in conversion")
			}
		})
	}
}

// 自動轉正之後輸出的 orientation 必須歸 1，否則看圖軟體會再轉一次。
func TestConvertResetsOrientationAfterAutoRotating(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "rotated.jpg")
	if err := os.WriteFile(src, jpegWithOrientation(t, makeImage(80, 40, true), 6), 0o644); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(dir, "out.jpg")
	if _, err := ConvertFile(src, dest, defaultRequest(FORMAT_JPEG)); err != nil {
		t.Fatalf("ConvertFile: %v", err)
	}

	info, err := Inspect(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Orientation > 1 {
		t.Errorf("orientation = %d; the image was already rotated, so viewers would rotate it twice", info.Orientation)
	}
	if info.Width != 40 || info.Height != 80 {
		t.Errorf("size = %dx%d, want 40x80", info.Width, info.Height)
	}
}

// 縮圖之後 EXIF 的尺寸欄位要跟著改，否則中繼資料在說謊。
func TestConvertUpdatesExifDimensionsAfterResizing(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.jpg")
	block := buildExifBlock(exifFixture{Orientation: 1, Width: 800, Height: 600, Make: "TestCam", DateTime: "2026:08:27 10:30:00"})
	if err := os.WriteFile(src, jpegWithExif(t, makeImage(800, 600, true), block), 0o644); err != nil {
		t.Fatal(err)
	}

	req := defaultRequest(FORMAT_JPEG)
	req.Resize = ResizeSpec{Width: 400, Mode: MODE_FIT}
	dest := filepath.Join(dir, "out.jpg")
	if _, err := ConvertFile(src, dest, req); err != nil {
		t.Fatalf("ConvertFile: %v", err)
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	got := parseHeader(FORMAT_JPEG, data).Exif
	ifd0, ok := readTIFF(got)
	if !ok {
		t.Fatal("output exif is not valid tiff")
	}
	if w, _ := ifd0.uint(got, TAG_IMAGE_WIDTH); w != 400 {
		t.Errorf("exif ImageWidth = %d, want 400", w)
	}
	if h, _ := ifd0.uint(got, TAG_IMAGE_LENGTH); h != 300 {
		t.Errorf("exif ImageLength = %d, want 300", h)
	}
}

// 要發佈到網路上時得能清掉 GPS 與相機資訊。
func TestConvertStripsExifWhenAsked(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.jpg")
	if err := os.WriteFile(src, jpegWithOrientation(t, makeImage(40, 20, true), 1), 0o644); err != nil {
		t.Fatal(err)
	}

	req := defaultRequest(FORMAT_JPEG)
	req.PreserveExif = false
	dest := filepath.Join(dir, "out.jpg")

	res, err := ConvertFile(src, dest, req)
	if err != nil {
		t.Fatalf("ConvertFile: %v", err)
	}
	if res.Exif {
		t.Error("result should not report exif when stripping")
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if got := parseHeader(FORMAT_JPEG, data).Exif; len(got) != 0 {
		t.Errorf("output still carries %d bytes of exif", len(got))
	}
}
