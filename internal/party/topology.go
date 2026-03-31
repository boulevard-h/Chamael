package party

import "Chamael/pkg/topology"

func localShardParams(mainN, workN, mainF, workF, shard uint32) (uint32, uint32) {
	if shard == 0 {
		return mainN, mainF
	}
	return workN, workF
}

func (p *HonestParty) TotalNodes() uint32 {
	return uint32(topology.TotalNodes(int(p.MainN), int(p.WorkN), int(p.M)))
}

func (p *HonestParty) ShardSize(shard uint32) uint32 {
	return uint32(topology.ShardSize(int(p.MainN), int(p.WorkN), int(p.M), int(shard)))
}

func (p *HonestParty) ShardFaults(shard uint32) uint32 {
	if shard == 0 {
		return p.MainF
	}
	return p.WorkF
}

func (p *HonestParty) ShardBounds(shard uint32) (uint32, uint32, bool) {
	start, end, ok := topology.ShardBounds(int(p.MainN), int(p.WorkN), int(p.M), int(shard))
	return uint32(start), uint32(end), ok
}

func (p *HonestParty) PIDToShardAndSID(pid uint32) (uint32, uint32, bool) {
	shard, sid, ok := topology.PIDToShardAndSID(int(p.MainN), int(p.WorkN), int(p.M), int(pid))
	return uint32(shard), uint32(sid), ok
}

func (p *HonestParty) SIDToPID(shard uint32, sid uint32) (uint32, bool) {
	pid, ok := topology.SIDToPID(int(p.MainN), int(p.WorkN), int(p.M), int(shard), int(sid))
	return uint32(pid), ok
}

func (p *HonestParty) IsPIDInShard(pid uint32, shard uint32) bool {
	actualShard, _, ok := p.PIDToShardAndSID(pid)
	return ok && actualShard == shard
}

func (p *CommonParty) TotalNodes() uint32 {
	return uint32(topology.TotalNodes(int(p.MainN), int(p.WorkN), int(p.m)))
}

func (p *CommonParty) ShardSize(shard uint32) uint32 {
	return uint32(topology.ShardSize(int(p.MainN), int(p.WorkN), int(p.m), int(shard)))
}

func (p *CommonParty) ShardBounds(shard uint32) (uint32, uint32, bool) {
	start, end, ok := topology.ShardBounds(int(p.MainN), int(p.WorkN), int(p.m), int(shard))
	return uint32(start), uint32(end), ok
}

func (p *CommonParty) PIDToShardAndSID(pid uint32) (uint32, uint32, bool) {
	shard, sid, ok := topology.PIDToShardAndSID(int(p.MainN), int(p.WorkN), int(p.m), int(pid))
	return uint32(shard), uint32(sid), ok
}

func (p *CommonParty) SIDToPID(shard uint32, sid uint32) (uint32, bool) {
	pid, ok := topology.SIDToPID(int(p.MainN), int(p.WorkN), int(p.m), int(shard), int(sid))
	return uint32(pid), ok
}
