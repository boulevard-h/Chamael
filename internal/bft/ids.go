package bft

import "encoding/binary"

// mvbaResultID = shard(uint32) || epoch(uint32)
func mvbaResultID(shard uint32, epoch uint32) []byte {
	id := make([]byte, 8)
	binary.BigEndian.PutUint32(id[0:4], shard)
	binary.BigEndian.PutUint32(id[4:8], epoch)
	return id
}

