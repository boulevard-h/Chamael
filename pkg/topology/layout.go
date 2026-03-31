package topology

func TotalNodes(mainN, workN, shardCount int) int {
	if shardCount <= 0 {
		return 0
	}
	total := mainN
	if shardCount > 1 {
		total += (shardCount - 1) * workN
	}
	return total
}

func ShardSize(mainN, workN, shardCount, shard int) int {
	if shard < 0 || shard >= shardCount {
		return 0
	}
	if shard == 0 {
		return mainN
	}
	return workN
}

func ShardStart(mainN, workN, shardCount, shard int) int {
	if shard < 0 || shard >= shardCount {
		return -1
	}
	if shard == 0 {
		return 0
	}
	return mainN + (shard-1)*workN
}

func ShardBounds(mainN, workN, shardCount, shard int) (int, int, bool) {
	start := ShardStart(mainN, workN, shardCount, shard)
	if start < 0 {
		return 0, 0, false
	}
	size := ShardSize(mainN, workN, shardCount, shard)
	return start, start + size, true
}

func PIDToShardAndSID(mainN, workN, shardCount, pid int) (int, int, bool) {
	total := TotalNodes(mainN, workN, shardCount)
	if pid < 0 || pid >= total {
		return 0, 0, false
	}
	if pid < mainN {
		return 0, pid, true
	}
	if workN <= 0 {
		return 0, 0, false
	}
	offset := pid - mainN
	shard := 1 + offset/workN
	if shard >= shardCount {
		return 0, 0, false
	}
	return shard, offset % workN, true
}

func SIDToPID(mainN, workN, shardCount, shard, sid int) (int, bool) {
	size := ShardSize(mainN, workN, shardCount, shard)
	if size <= 0 || sid < 0 || sid >= size {
		return 0, false
	}
	start := ShardStart(mainN, workN, shardCount, shard)
	if start < 0 {
		return 0, false
	}
	return start + sid, true
}
