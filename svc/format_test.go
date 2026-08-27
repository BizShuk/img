package svc

import "testing"

func TestParseFormatAcceptsAliasesAndRejectsOthers(t *testing.T) {
	cases := []struct {
		in      string
		want    Format
		wantErr bool
	}{
		{"jpg", FORMAT_JPEG, false},
		{"JPEG", FORMAT_JPEG, false},
		{".jpg", FORMAT_JPEG, false},
		{" png ", FORMAT_PNG, false},
		{"webp", FORMAT_WEBP, false},
		{"gif", FORMAT_GIF, false},
		{"tif", FORMAT_TIFF, false},
		{"heif", FORMAT_HEIC, false},
		{"avif", FORMAT_AVIF, false},
		{"bmp", "", true},
		{"", "", true},
	}

	for _, tc := range cases {
		got, err := ParseFormat(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("ParseFormat(%q) err = %v, wantErr %v", tc.in, err, tc.wantErr)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseFormat(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// convert 的 -f 只能收能寫出來的格式。gif / tiff 讀得進來但寫不出去，
// heic / avif 連讀都不行 —— 三者都必須在旗標層就被擋下。
func TestParseTargetFormatOnlyAcceptsEncodable(t *testing.T) {
	for _, in := range []string{"jpg", "png", "webp"} {
		if _, err := ParseTargetFormat(in); err != nil {
			t.Errorf("ParseTargetFormat(%q) unexpected error: %v", in, err)
		}
	}
	for _, in := range []string{"gif", "tiff", "heic", "avif", "bmp"} {
		if _, err := ParseTargetFormat(in); err == nil {
			t.Errorf("ParseTargetFormat(%q) should fail", in)
		}
	}
}

func TestFormatCapabilities(t *testing.T) {
	cases := []struct {
		f                    Format
		encodable, decodable bool
	}{
		{FORMAT_JPEG, true, true},
		{FORMAT_PNG, true, true},
		{FORMAT_WEBP, true, true},
		{FORMAT_GIF, false, true},
		{FORMAT_TIFF, false, true},
		{FORMAT_HEIC, false, false},
		{FORMAT_AVIF, false, false},
	}
	for _, tc := range cases {
		if got := tc.f.Encodable(); got != tc.encodable {
			t.Errorf("%s.Encodable() = %v, want %v", tc.f, got, tc.encodable)
		}
		if got := tc.f.Decodable(); got != tc.decodable {
			t.Errorf("%s.Decodable() = %v, want %v", tc.f, got, tc.decodable)
		}
	}
}

// HEIC 與 AVIF 共用 ISO BMFF 容器，差別只在 ftyp 的品牌識別碼，
// 而品牌可能只出現在相容清單裡而不是 major brand。
func TestSniffUndecodableReadsFtypBrands(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want Format
	}{
		{"major heic", fakeHEIC("heic", "mif1", "heic"), FORMAT_HEIC},
		{"major mif1", fakeHEIC("mif1", "heic"), FORMAT_HEIC},
		{"major avif", fakeHEIC("avif", "mif1", "miaf"), FORMAT_AVIF},
		{"avif wins over heic", fakeHEIC("mif1", "avif"), FORMAT_AVIF},
		{"unrelated brand", fakeHEIC("mp42", "isom"), ""},
		{"too short", []byte("RIFF"), ""},
		{"not a box", make([]byte, 64), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SniffUndecodable(tc.data); got != tc.want {
				t.Errorf("SniffUndecodable = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExtAndMIMEAreConsistent(t *testing.T) {
	cases := map[Format]struct{ ext, mime string }{
		FORMAT_JPEG: {".jpg", "image/jpeg"},
		FORMAT_PNG:  {".png", "image/png"},
		FORMAT_WEBP: {".webp", "image/webp"},
	}
	for f, want := range cases {
		if got := f.Ext(); got != want.ext {
			t.Errorf("%s.Ext() = %q, want %q", f, got, want.ext)
		}
		if got := f.MIME(); got != want.mime {
			t.Errorf("%s.MIME() = %q, want %q", f, got, want.mime)
		}
	}
}

// 格式必須由內容決定，副檔名說謊時仍要判對 —— 這是 analyze 的 mismatch
// 欄位能成立的前提。
func TestDecodeIgnoresLyingExtension(t *testing.T) {
	img := makeImage(24, 16, true)
	for _, f := range EncodableFormats {
		data := encodeTo(t, img, f, EncodeOptions{Quality: 80})

		_, got, err := Decode(data)
		if err != nil {
			t.Fatalf("decode %s: %v", f, err)
		}
		if got != f {
			t.Errorf("Decode detected %q, want %q", got, f)
		}
	}
}

func TestFormatFromExtReturnsEmptyForUnknown(t *testing.T) {
	if got := FormatFromExt("a.txt"); got != "" {
		t.Errorf("FormatFromExt(a.txt) = %q, want empty", got)
	}
	if got := FormatFromExt("a.HEIC"); got != FORMAT_HEIC {
		t.Errorf("FormatFromExt(a.HEIC) = %q, want heic", got)
	}
	if got := FormatFromExt("a.JPEG"); got != FORMAT_JPEG {
		t.Errorf("FormatFromExt(a.JPEG) = %q, want jpeg", got)
	}
}
