package config

func normalizeTiming(prepare, waitEpoch, waitBuf *int, legacyPrepareTime, legacyWaitTime int) {
	if *prepare == 0 && legacyPrepareTime > 0 {
		*prepare = legacyPrepareTime / 10
	}
	if *waitEpoch == 0 && legacyWaitTime > 0 {
		*waitEpoch = maxInt(1, legacyWaitTime/10)
	}
	if *waitBuf == 0 && legacyWaitTime > 0 {
		*waitBuf = legacyWaitTime / 3
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
