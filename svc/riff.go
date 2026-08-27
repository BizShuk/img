package svc

import "encoding/binary"

// WebP 的 VP8X 旗標位元。一個檔案同時帶透明與 EXIF 時兩個位元都要設。
const (
	WEBP_FLAG_ANIMATION = 0x02
	WEBP_FLAG_XMP       = 0x04
	WEBP_FLAG_EXIF      = 0x08
	WEBP_FLAG_ALPHA     = 0x10
	WEBP_FLAG_ICC       = 0x20
)

// exifPrefix 是 JPEG APP1 段落用來標示 EXIF 的前綴。PNG 的 eXIf 與 WebP 的
// EXIF chunk 規格上不帶它，但實務上有寫入器會加，因此讀取時一律先剝掉。
var exifPrefix = []byte("Exif\x00\x00")

// riffChunk 是 RIFF 容器裡的一段具名資料。
type riffChunk struct {
	Name    string
	Payload []byte
}

// forEachRIFFChunk 依序走訪 WebP 的 chunk。fn 回傳 false 即停止。
// RIFF 的欄位順序是 FourCC 在前、長度在後，與 PNG 相反；奇數長度的
// chunk 後面還要補一個 pad byte，漏掉會讓後續 chunk 全部錯位。
func forEachRIFFChunk(data []byte, fn func(name string, payload []byte) bool) {
	if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return
	}

	for i := 12; i+8 <= len(data); {
		name := string(data[i : i+4])
		size := int(binary.LittleEndian.Uint32(data[i+4 : i+8]))
		if size < 0 || i+8+size > len(data) {
			return
		}
		if !fn(name, data[i+8:i+8+size]) {
			return
		}

		i += 8 + size
		if size%2 == 1 {
			i++
		}
	}
}

// riffChunks 把 WebP 拆成 chunk 清單。
func riffChunks(data []byte) []riffChunk {
	var chunks []riffChunk
	forEachRIFFChunk(data, func(name string, payload []byte) bool {
		chunks = append(chunks, riffChunk{Name: name, Payload: payload})
		return true
	})
	return chunks
}

// buildWebP 由 chunk 清單組回一個完整的 WebP 檔案。
func buildWebP(chunks []riffChunk) []byte {
	body := make([]byte, 0, 64)
	body = append(body, "WEBP"...)

	for _, c := range chunks {
		var header [8]byte
		copy(header[0:4], c.Name)
		binary.LittleEndian.PutUint32(header[4:8], uint32(len(c.Payload)))
		body = append(body, header[:]...)
		body = append(body, c.Payload...)
		if len(c.Payload)%2 == 1 {
			body = append(body, 0)
		}
	}

	out := make([]byte, 0, len(body)+8)
	out = append(out, "RIFF"...)
	var size [4]byte
	binary.LittleEndian.PutUint32(size[:], uint32(len(body)))
	out = append(out, size[:]...)
	return append(out, body...)
}

// newVP8XChunk 造一個延伸格式的檔頭。簡易的單張 VP8 / VP8L 檔案沒有
// VP8X，要掛上 EXIF 就必須先補一個，否則旗標無處可放。
func newVP8XChunk(flags byte, width, height int) riffChunk {
	payload := make([]byte, 10)
	payload[0] = flags
	putUint24(payload[4:7], uint32(width-1))
	putUint24(payload[7:10], uint32(height-1))
	return riffChunk{Name: "VP8X", Payload: payload}
}

// putUint24 寫入 WebP 畫布尺寸用的 3 位元組小端整數。
func putUint24(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
}
