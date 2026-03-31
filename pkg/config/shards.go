package config

import (
	"Chamael/pkg/topology"
	"fmt"
)

func normalizeShardConfig(legacyN, legacyF int, mainN, workN, mainF, workF *int) {
	if *mainN <= 0 && legacyN > 0 {
		*mainN = legacyN
	}
	if *workN <= 0 {
		switch {
		case legacyN > 0:
			*workN = legacyN
		case *mainN > 0:
			*workN = *mainN
		}
	}

	if *mainF == 0 && legacyF > 0 {
		*mainF = legacyF
	}
	if *workF == 0 && legacyF > 0 {
		*workF = legacyF
	}
}

func validateShardConfig(mainN, workN, mainF, workF, shardCount int) error {
	if mainN <= 0 {
		return fmt.Errorf("N_M must be positive")
	}
	if workN <= 0 {
		return fmt.Errorf("N_W must be positive")
	}
	if shardCount <= 0 {
		return fmt.Errorf("m must be positive")
	}
	if mainF < 0 {
		return fmt.Errorf("F_M must be non-negative")
	}
	if workF < 0 {
		return fmt.Errorf("F_W must be non-negative")
	}
	if mainF >= mainN {
		return fmt.Errorf("F_M must be smaller than N_M")
	}
	if shardCount > 1 && workF >= workN {
		return fmt.Errorf("F_W must be smaller than N_W")
	}
	if 3*mainF+1 > mainN {
		return fmt.Errorf("N_M must satisfy N_M >= 3*F_M+1")
	}
	if shardCount > 1 && 3*workF+1 > workN {
		return fmt.Errorf("N_W must satisfy N_W >= 3*F_W+1")
	}
	return nil
}

func totalNodes(mainN, workN, shardCount int) int {
	return topology.TotalNodes(mainN, workN, shardCount)
}

func shardSize(mainN, workN, shardCount, shard int) int {
	return topology.ShardSize(mainN, workN, shardCount, shard)
}

func shardStart(mainN, workN, shardCount, shard int) int {
	return topology.ShardStart(mainN, workN, shardCount, shard)
}

func pidToShardAndSID(mainN, workN, shardCount, pid int) (int, int, bool) {
	return topology.PIDToShardAndSID(mainN, workN, shardCount, pid)
}
