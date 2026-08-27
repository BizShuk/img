package svc

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ConvertRequest 是一次 convert 執行的完整參數。
type ConvertRequest struct {
	Target     Format
	Resize     ResizeSpec
	Encode     EncodeOptions
	AutoRotate bool
	// PreserveExif 把來源的 EXIF 區塊（拍攝時間、相機、鏡頭、GPS）
	// 帶進輸出檔。關掉即等同於清除中繼資料。
	PreserveExif bool
	Overwrite    bool
	// Output 是使用者給的 -o。空字串表示輸出到原檔旁邊；
	// 指向目錄時逐檔輸出，指向檔名時只允許單一輸入。
	Output string
}

// Result 是單一檔案的轉換結果，供 CLI 呈現與測試斷言。
type Result struct {
	Source      string `json:"source"`
	Dest        string `json:"dest"`
	Format      Format `json:"format"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Bytes       int64  `json:"bytes"`
	SourceBytes int64  `json:"source_bytes"`
	Exif        bool   `json:"exif"`
}

// isImageExt 判斷副檔名看起來像不像影像。掃描目錄時用它過濾，只是為了
// 避免開啟目錄下每一個無關檔案；被選中的檔案仍以 magic bytes 判定格式。
func isImageExt(ext string) bool {
	_, ok := formatNames[strings.ToLower(strings.TrimPrefix(ext, "."))]
	return ok
}

// Convert 逐一轉換輸入路徑（檔案或目錄）。單一檔案失敗不中斷其餘檔案。
func Convert(inputs []string, req ConvertRequest) ([]Result, []error) {
	sources, errs := ExpandInputs(inputs)
	if len(sources) == 0 {
		return nil, errs
	}

	dests, err := resolveDests(sources, req)
	if err != nil {
		return nil, append(errs, err)
	}

	results := make([]Result, 0, len(sources))
	for i, src := range sources {
		res, err := ConvertFile(src, dests[i], req)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", src, err))
			continue
		}
		results = append(results, res)
	}
	return results, errs
}

// ConvertFile 執行單一檔案的轉換管線：解碼 -> 依 EXIF 轉正 -> 縮放 -> 編碼。
func ConvertFile(src, dest string, req ConvertRequest) (Result, error) {
	stat, err := os.Stat(src)
	if err != nil {
		return Result{}, err
	}

	if !req.Overwrite {
		if _, err := os.Stat(dest); err == nil {
			return Result{}, fmt.Errorf("%s already exists (use --overwrite)", dest)
		}
	}

	data, err := os.ReadFile(src)
	if err != nil {
		return Result{}, err
	}

	img, sourceFormat, err := Decode(data)
	if err != nil {
		return Result{}, err
	}
	meta := parseHeader(sourceFormat, data)

	rotated := req.AutoRotate && meta.Orientation > 1
	if req.AutoRotate {
		img = ApplyOrientation(img, meta.Orientation)
	}
	img = Resize(img, req.Resize)
	b := img.Bounds()

	var buf bytes.Buffer
	if err := Encode(&buf, img, req.Target, req.Encode); err != nil {
		return Result{}, err
	}
	encoded := buf.Bytes()

	carriedExif := false
	if req.PreserveExif && len(meta.Exif) > 0 {
		block := RetargetExif(meta.Exif, b.Dx(), b.Dy(), rotated)
		withExif, err := EmbedExif(encoded, req.Target, block, b.Dx(), b.Dy())
		if err != nil {
			// EXIF 帶不過去不該讓整張圖轉換失敗；影像本身是主要產物。
			slog.Warn("exif not carried over",
				"source", src, "dest", dest, "target", string(req.Target), "reason", err)
		} else {
			encoded = withExif
			carriedExif = true
		}
	}

	if err := writeAtomically(dest, encoded); err != nil {
		return Result{}, err
	}

	return Result{
		Source:      src,
		Dest:        dest,
		Format:      req.Target,
		Width:       b.Dx(),
		Height:      b.Dy(),
		Bytes:       int64(len(encoded)),
		SourceBytes: stat.Size(),
		Exif:        carriedExif,
	}, nil
}

// writeAtomically 先寫同目錄的暫存檔再改名。寫到一半失敗不會留下半個
// 檔案 —— 尤其是 dest 與來源相同（原地轉換）的情況。
func writeAtomically(dest string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(dest), ".img-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dest)
}

// ExpandInputs 把使用者給的路徑展開成檔案清單。目錄只掃該層，不遞迴。
func ExpandInputs(inputs []string) ([]string, []error) {
	var files []string
	var errs []error

	for _, in := range inputs {
		stat, err := os.Stat(in)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !stat.IsDir() {
			files = append(files, in)
			continue
		}

		entries, err := os.ReadDir(in)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		found := 0
		for _, e := range entries {
			if e.IsDir() || !isImageExt(filepath.Ext(e.Name())) {
				continue
			}
			files = append(files, filepath.Join(in, e.Name()))
			found++
		}
		if found == 0 {
			errs = append(errs, fmt.Errorf("%s: no image files found", in))
		}
	}

	sort.Strings(files)
	return files, errs
}

// resolveDests 決定每個來源檔的輸出路徑。
func resolveDests(sources []string, req ConvertRequest) ([]string, error) {
	dests := make([]string, len(sources))

	if req.Output == "" {
		for i, src := range sources {
			dests[i] = replaceExt(src, req.Target.Ext())
		}
		return dests, nil
	}

	if isDirTarget(req.Output) {
		for i, src := range sources {
			dests[i] = filepath.Join(req.Output, replaceExt(filepath.Base(src), req.Target.Ext()))
		}
		return dests, nil
	}

	if len(sources) > 1 {
		return nil, fmt.Errorf("-o %s names a single file but %d inputs were given; pass a directory instead", req.Output, len(sources))
	}
	dests[0] = req.Output
	return dests, nil
}

// isDirTarget 判斷 -o 該當成目錄看待。路徑已存在時直接看它是不是目錄；
// 還不存在時，帶影像副檔名的視為檔名，其餘（含以分隔符結尾者）視為待建目錄。
func isDirTarget(output string) bool {
	if strings.HasSuffix(output, string(os.PathSeparator)) {
		return true
	}
	if stat, err := os.Stat(output); err == nil {
		return stat.IsDir()
	}
	return !isImageExt(filepath.Ext(output))
}

// replaceExt 換掉副檔名，保留其餘檔名。
func replaceExt(path, ext string) string {
	return strings.TrimSuffix(path, filepath.Ext(path)) + ext
}
