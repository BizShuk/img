package cmd

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bizshuk/img/config"
	"github.com/bizshuk/img/svc"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// TestMain 只放設定預設值，不呼叫 config.Default —— 那會在使用者的
// ~/.config/img/ 落下檔案，測試不該有這種副作用。
func TestMain(m *testing.M) {
	viper.SetDefault(config.KEY_QUALITY, 82)
	viper.SetDefault(config.KEY_WEBP_METHOD, 4)
	viper.SetDefault(config.KEY_RESIZE_MODE, "fit")
	os.Exit(m.Run())
}

// run 從根命令執行一個子命令，回傳 stdout 內容。cobra 的子命令一律經由
// 根命令解析，旗標又綁在套件層變數上，因此每次執行前先把旗標歸零，
// 否則前一個測試設過的值會滲進來。
func run(t *testing.T, sub *cobra.Command, args ...string) (string, error) {
	t.Helper()
	for _, c := range []*cobra.Command{AnalyzeCmd, ConvertCmd} {
		c.Flags().VisitAll(func(f *pflag.Flag) {
			_ = f.Value.Set(f.DefValue)
			f.Changed = false
		})
	}

	var out, errOut bytes.Buffer
	RootCmd.SetOut(&out)
	RootCmd.SetErr(&errOut)
	RootCmd.SetArgs(append([]string{sub.Name()}, args...))
	err := RootCmd.Execute()
	return out.String(), err
}

func seedPNG(t *testing.T, dir, name string, w, h int) string {
	t.Helper()
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			m.Set(x, y, color.NRGBA{uint8(x % 256), uint8(y % 256), 128, 255})
		}
	}

	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, m); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAnalyzeJSONIsMachineReadable(t *testing.T) {
	dir := t.TempDir()
	src := seedPNG(t, dir, "a.png", 64, 48)

	out, err := run(t, AnalyzeCmd, src, "--json")
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}

	var infos []svc.Info
	if err := json.Unmarshal([]byte(out), &infos); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if len(infos) != 1 {
		t.Fatalf("got %d entries, want 1", len(infos))
	}
	if infos[0].Width != 64 || infos[0].Height != 48 || infos[0].Format != svc.FORMAT_PNG {
		t.Errorf("unexpected info: %+v", infos[0])
	}
}

func TestAnalyzeTableListsEveryFile(t *testing.T) {
	dir := t.TempDir()
	a := seedPNG(t, dir, "a.png", 10, 10)
	b := seedPNG(t, dir, "b.png", 20, 20)

	out, err := run(t, AnalyzeCmd, a, b)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if strings.Count(out, "format    png") != 2 {
		t.Errorf("expected two entries, got:\n%s", out)
	}
}

func TestAnalyzeFailsWhenNothingCouldBeRead(t *testing.T) {
	if _, err := run(t, AnalyzeCmd, filepath.Join(t.TempDir(), "missing.png")); err == nil {
		t.Error("analyze should fail when no file could be inspected")
	}
}

func TestConvertWritesIntoADirectory(t *testing.T) {
	dir := t.TempDir()
	seedPNG(t, dir, "a.png", 64, 48)
	seedPNG(t, dir, "b.png", 64, 48)
	out := filepath.Join(dir, "converted")

	if _, err := run(t, ConvertCmd, dir, "-f", "webp", "-o", out, "-w", "32"); err != nil {
		t.Fatalf("convert: %v", err)
	}

	for _, name := range []string{"a.webp", "b.webp"} {
		info, err := svc.Inspect(filepath.Join(out, name))
		if err != nil {
			t.Fatalf("inspect %s: %v", name, err)
		}
		if info.Width != 32 || info.Format != svc.FORMAT_WEBP {
			t.Errorf("%s = %dx%d %s, want 32 wide webp", name, info.Width, info.Height, info.Format)
		}
	}
}

func TestConvertRejectsInvalidFlagCombinations(t *testing.T) {
	dir := t.TempDir()
	src := seedPNG(t, dir, "a.png", 16, 16)

	cases := []struct {
		name string
		args []string
	}{
		{"unknown format", []string{src, "-f", "gif"}},
		{"lossless outside webp", []string{src, "-f", "jpg", "--lossless"}},
		{"scale with width", []string{src, "-f", "png", "--scale", "0.5", "-w", "10"}},
		{"cover without both sides", []string{src, "-f", "png", "--mode", "cover", "-w", "10"}},
		{"unknown mode", []string{src, "-f", "png", "--mode", "squish"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := run(t, ConvertCmd, tc.args...); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestConvertFallsBackToConfiguredQuality(t *testing.T) {
	dir := t.TempDir()
	src := seedPNG(t, dir, "a.png", 64, 48)

	viper.Set(config.KEY_QUALITY, 10)
	defer viper.Set(config.KEY_QUALITY, 82)

	if _, err := run(t, ConvertCmd, src, "-f", "jpg", "-o", filepath.Join(dir, "low.jpg")); err != nil {
		t.Fatalf("convert: %v", err)
	}
	if _, err := run(t, ConvertCmd, src, "-f", "jpg", "-q", "95", "-o", filepath.Join(dir, "high.jpg")); err != nil {
		t.Fatalf("convert: %v", err)
	}

	low, err := os.Stat(filepath.Join(dir, "low.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	high, err := os.Stat(filepath.Join(dir, "high.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	if low.Size() >= high.Size() {
		t.Errorf("quality 10 produced %d bytes, quality 95 produced %d; the configured default was ignored", low.Size(), high.Size())
	}
}

// analyze 對解不開的格式仍要講出格式名，而不是整批失敗。
func TestAnalyzeNamesUndecodableFormats(t *testing.T) {
	dir := t.TempDir()
	heic := filepath.Join(dir, "photo.heic")
	// 只有 ftyp box 的 ISO BMFF 檔頭：足以辨識，不足以解碼。
	if err := os.WriteFile(heic, []byte("\x00\x00\x00\x14ftypheic\x00\x00\x00\x00mif1"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := run(t, AnalyzeCmd, heic, "--json")
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}

	var infos []svc.Info
	if err := json.Unmarshal([]byte(out), &infos); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if len(infos) != 1 || infos[0].Format != svc.FORMAT_HEIC || infos[0].Decodable {
		t.Fatalf("unexpected info: %+v", infos)
	}

	table, err := run(t, AnalyzeCmd, heic)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if !strings.Contains(table, "format    heic") || !strings.Contains(table, "note") {
		t.Errorf("table output should name the format and explain the gap:\n%s", table)
	}
}

// convert 的 -f 只收能寫出來的格式；讀得進來的 gif / tiff 也不行。
func TestConvertRejectsNonEncodableTargets(t *testing.T) {
	dir := t.TempDir()
	src := seedPNG(t, dir, "a.png", 16, 16)

	for _, target := range []string{"gif", "tiff", "heic"} {
		t.Run(target, func(t *testing.T) {
			if _, err := run(t, ConvertCmd, src, "-f", target); err == nil {
				t.Errorf("-f %s should be rejected", target)
			}
		})
	}
}

// --strip-exif 是發佈前清掉 GPS 與相機資訊的開關，預設保留。
func TestConvertStripExifFlag(t *testing.T) {
	dir := t.TempDir()
	src := seedPNG(t, dir, "a.png", 32, 32)

	kept := filepath.Join(dir, "kept.jpg")
	if _, err := run(t, ConvertCmd, src, "-f", "jpg", "-o", kept); err != nil {
		t.Fatalf("convert: %v", err)
	}
	stripped := filepath.Join(dir, "stripped.jpg")
	if _, err := run(t, ConvertCmd, src, "-f", "jpg", "--strip-exif", "-o", stripped); err != nil {
		t.Fatalf("convert: %v", err)
	}

	// 來源是沒有 EXIF 的 PNG，兩邊都不該憑空生出中繼資料。
	for _, path := range []string{kept, stripped} {
		info, err := svc.Inspect(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Orientation != svc.ORIENTATION_NONE {
			t.Errorf("%s gained an orientation tag out of nowhere", filepath.Base(path))
		}
	}
}
