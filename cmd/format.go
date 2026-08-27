package cmd

import (
	"fmt"
	"strconv"
)

// humanBytes 把位元組數寫成人看得懂的單位。
func humanBytes(n int64) string {
	const UNIT = 1024
	if n < UNIT {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(UNIT), 0
	for v := n / UNIT; v >= UNIT; v /= UNIT {
		div *= UNIT
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}

// humanCount 為整數加上千分位，讓像素總數可讀。
func humanCount(n int) string {
	s := strconv.Itoa(n)
	if n < 0 {
		return "-" + humanCount(-n)
	}
	head := len(s) % 3
	if head == 0 {
		head = 3
	}
	out := s[:head]
	for i := head; i < len(s); i += 3 {
		out += "," + s[i:i+3]
	}
	return out
}
