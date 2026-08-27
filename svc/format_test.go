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
		{"gif", "", true},
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
	for _, f := range SupportedFormats {
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
	if got := FormatFromExt("a.gif"); got != "" {
		t.Errorf("FormatFromExt(a.gif) = %q, want empty", got)
	}
	if got := FormatFromExt("a.JPEG"); got != FORMAT_JPEG {
		t.Errorf("FormatFromExt(a.JPEG) = %q, want jpeg", got)
	}
}
