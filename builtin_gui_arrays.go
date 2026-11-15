package main

// DLL配列引数 (int[]/float[]) で使う数値変換。
func numValue(v Value) (float64, bool) {
	switch v.K {
	case KInt:
		return float64(v.I), true
	case KFloat:
		return v.F, true
	}
	return 0, false
}
