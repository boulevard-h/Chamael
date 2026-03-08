package party

import (
	"fmt"
	"strings"
	"time"
)

const sendEnqueueTimeout = 5 * time.Second

func formatBroadcastError(scope string, failed []uint32, total int) error {
	if len(failed) == 0 {
		return nil
	}

	parts := make([]string, 0, len(failed))
	for _, des := range failed {
		parts = append(parts, fmt.Sprintf("%d", des))
	}

	return fmt.Errorf("%s failed for %d/%d destinations: %s", scope, len(failed), total, strings.Join(parts, ","))
}
