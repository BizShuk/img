package svc

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
)

// ErrExifTooLarge 表示 EXIF 區塊塞不進目標容器。呼叫端可以據此改用
// CompactExif 重建一份較小的再試一次，而不是直接放棄整段中繼資料。
var ErrExifTooLarge = errors.New("exif block exceeds the container segment limit")

// JPEG_SEGMENT_MAX 是一個 JPEG marker segment 的長度上限。長度欄位本身
// 佔 2 個位元組且計入其中，因此 payload 最多 65533。
const JPEG_SEGMENT_MAX = 0xFFFF

// EmbedExif 把一段 EXIF（原始 TIFF 區塊）寫進已編碼好的影像位元組。
// block 為空時原樣回傳。三種容器各有自己的擺放規則，但共通點是
// EXIF 都以未經修改的 TIFF 區塊儲存，只有外層包裝不同。
func EmbedExif(encoded []byte, f Format, block []byte, width, height int) ([]byte, error) {
	if len(block) == 0 {
		return encoded, nil
	}

	switch f {
	case FORMAT_JPEG:
		return embedExifJPEG(encoded, block)
	case FORMAT_PNG:
		return embedExifPNG(encoded, block)
	case FORMAT_WEBP:
		return embedExifWebP(encoded, block, width, height)
	default:
		return encoded, fmt.Errorf("cannot embed exif into %s", f)
	}
}

// embedExifJPEG 插入一個 APP1 段落。位置放在 SOI 與既有的 APP0 (JFIF)
// 之後 —— 這是實務上最常見的排列，相容性最好。
func embedExifJPEG(encoded, block []byte) ([]byte, error) {
	payload := append(append([]byte{}, exifPrefix...), block...)
	if len(payload)+2 > JPEG_SEGMENT_MAX {
		return encoded, fmt.Errorf("%w: %d bytes, jpeg allows %d", ErrExifTooLarge, len(payload), JPEG_SEGMENT_MAX-2)
	}
	if len(encoded) < 2 {
		return encoded, fmt.Errorf("not a jpeg")
	}

	at := 2 // SOI 之後
	if len(encoded) >= 6 && encoded[2] == 0xFF && encoded[3] == 0xE0 {
		at += 2 + int(binary.BigEndian.Uint16(encoded[4:6]))
		if at > len(encoded) {
			return encoded, fmt.Errorf("malformed app0 segment")
		}
	}

	segment := make([]byte, 0, len(payload)+4)
	segment = append(segment, 0xFF, 0xE1)
	var size [2]byte
	binary.BigEndian.PutUint16(size[:], uint16(len(payload)+2))
	segment = append(segment, size[:]...)
	segment = append(segment, payload...)

	out := make([]byte, 0, len(encoded)+len(segment))
	out = append(out, encoded[:at]...)
	out = append(out, segment...)
	return append(out, encoded[at:]...), nil
}

// embedExifPNG 在第一個 IDAT 之前插入一個 eXIf chunk。規格允許放在
// IDAT 前後，但放在前面讀取端才不必為了拿中繼資料掃完整份影像資料。
func embedExifPNG(encoded, block []byte) ([]byte, error) {
	at := -1
	for i := 8; i+8 <= len(encoded); {
		size := int(binary.BigEndian.Uint32(encoded[i : i+4]))
		if size < 0 || i+12+size > len(encoded) {
			break
		}
		if name := string(encoded[i+4 : i+8]); name == "IDAT" || name == "IEND" {
			at = i
			break
		}
		i += 12 + size
	}
	if at < 0 {
		return encoded, fmt.Errorf("no IDAT or IEND chunk found")
	}

	chunk := make([]byte, 0, len(block)+12)
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(block)))
	chunk = append(chunk, size[:]...)
	chunk = append(chunk, "eXIf"...)
	chunk = append(chunk, block...)
	var sum [4]byte
	binary.BigEndian.PutUint32(sum[:], crc32.ChecksumIEEE(chunk[4:]))
	chunk = append(chunk, sum[:]...)

	out := make([]byte, 0, len(encoded)+len(chunk))
	out = append(out, encoded[:at]...)
	out = append(out, chunk...)
	return append(out, encoded[at:]...), nil
}

// embedExifWebP 重組 RIFF 容器。簡易的單張 VP8 / VP8L 檔案沒有 VP8X，
// 得先補一個延伸檔頭才有地方放 EXIF 旗標；已經是延伸格式的則只要
// 把旗標補上並在尾端接一個 EXIF chunk。
func embedExifWebP(encoded, block []byte, width, height int) ([]byte, error) {
	chunks := riffChunks(encoded)
	if len(chunks) == 0 {
		return encoded, fmt.Errorf("not a webp")
	}

	// 既有的 EXIF chunk 一律換掉，不留兩份互相矛盾的中繼資料。
	kept := make([]riffChunk, 0, len(chunks)+1)
	for _, c := range chunks {
		if c.Name == "EXIF" {
			continue
		}
		kept = append(kept, c)
	}

	if kept[0].Name == "VP8X" {
		if len(kept[0].Payload) < 1 {
			return encoded, fmt.Errorf("malformed VP8X chunk")
		}
		payload := append([]byte{}, kept[0].Payload...)
		payload[0] |= WEBP_FLAG_EXIF
		kept[0].Payload = payload
	} else {
		flags := byte(WEBP_FLAG_EXIF)
		if parseWebPHeader(encoded).HasAlpha {
			flags |= WEBP_FLAG_ALPHA
		}
		kept = append([]riffChunk{newVP8XChunk(flags, width, height)}, kept...)
	}

	kept = append(kept, riffChunk{Name: "EXIF", Payload: block})
	return buildWebP(kept), nil
}
