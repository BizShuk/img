package svc

import (
	"fmt"
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
	Overwrite  bool
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
}

// imageExts 是掃描目錄時採信的副檔名。這裡用副檔名過濾只是為了避免
// 開啟目錄下每一個無關檔案；被選中的檔案仍以 magic bytes 判定格式。
var imageExts = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true}

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

	if req.AutoRotate {
		img = ApplyOrientation(img, parseHeader(sourceFormat, data).Orientation)
	}
	img = Resize(img, req.Resize)

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return Result{}, err
	}

	// 先寫暫存檔再改名，避免轉換途中失敗留下半個檔案 —— 尤其是
	// dest 與 src 相同（原地轉換）的情況。
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".img-*")
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(tmp.Name())

	if err := Encode(tmp, img, req.Target, req.Encode); err != nil {
		tmp.Close()
		return Result{}, err
	}
	if err := tmp.Close(); err != nil {
		return Result{}, err
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		return Result{}, err
	}

	out, err := os.Stat(dest)
	if err != nil {
		return Result{}, err
	}

	b := img.Bounds()
	return Result{
		Source:      src,
		Dest:        dest,
		Format:      req.Target,
		Width:       b.Dx(),
		Height:      b.Dy(),
		Bytes:       out.Size(),
		SourceBytes: stat.Size(),
	}, nil
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
			if e.IsDir() || !imageExts[strings.ToLower(filepath.Ext(e.Name()))] {
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
	return !imageExts[strings.ToLower(filepath.Ext(output))]
}

// replaceExt 換掉副檔名，保留其餘檔名。
func replaceExt(path, ext string) string {
	return strings.TrimSuffix(path, filepath.Ext(path)) + ext
}
