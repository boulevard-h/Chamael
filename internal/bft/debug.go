package bft

import (
	"fmt"

	"Areopagus/internal/party"
)

const ansiReset = "\x1b[0m"

var shardAnsiColors = [...]string{
	"\x1b[97m", // shard 0: bright white
	"\x1b[36m", // shard 1: cyan
	"\x1b[35m", // shard 2: magenta
	"\x1b[34m", // shard 3: blue
	"\x1b[33m", // shard 4: yellow
	"\x1b[32m", // shard 5: green
	"\x1b[31m", // shard 6: red
	"\x1b[96m", // shard 7: bright cyan
	"\x1b[95m", // shard 8: bright magenta
	"\x1b[94m", // shard 9: bright blue
	"\x1b[93m", // shard 10: bright yellow
	"\x1b[92m", // shard 11: bright green
	"\x1b[91m", // shard 12: bright red
}

func shardColor(shard uint32) string {
	return shardAnsiColors[shard%uint32(len(shardAnsiColors))]
}

func Debugf(p *party.HonestParty, format string, args ...any) {
	if p == nil || !p.Debug {
		return
	}
	color := shardColor(p.Snumber)
	prefix := fmt.Sprintf("%s[shard %d node %d]%s ", color, p.Snumber, p.PID, ansiReset)
	fmt.Printf(prefix+format+"\n", args...)
}
