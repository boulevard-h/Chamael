package bft

func rbcBitmapStreamID() []byte {
	return []byte("RBC_Bitmap_STREAM")
}

func bitmapLenBits(n uint32) int {
	return int((n + 7) / 8)
}

func bitmapSet(bm []byte, bit uint32) {
	byteIdx := bit / 8
	if int(byteIdx) >= len(bm) {
		return
	}
	bm[byteIdx] |= 1 << (bit % 8)
}

func bitmapGet(bm []byte, bit uint32) bool {
	byteIdx := bit / 8
	if int(byteIdx) >= len(bm) {
		return false
	}
	return (bm[byteIdx] & (1 << (bit % 8))) != 0
}

func bitmapOnes(bm []byte, n uint32) []uint32 {
	out := make([]uint32, 0)
	for i := uint32(0); i < n; i++ {
		if bitmapGet(bm, i) {
			out = append(out, i)
		}
	}
	return out
}

func bitmapCountOnes(bm []byte, n uint32) int {
	c := 0
	for i := uint32(0); i < n; i++ {
		if bitmapGet(bm, i) {
			c++
		}
	}
	return c
}
