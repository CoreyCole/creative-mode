package world

import "fmt"

// LoadCheckpointExpr returns a data-on-click expression that calls
// the loadCheckpoint JS function with the given IDs.
func LoadCheckpointExpr(worldID, cpID string) string {
	return fmt.Sprintf("loadCheckpoint('%s','%s')", worldID, cpID)
}

// LoadLineageExpr returns a data-on-click expression that reads
// current signal values and calls loadLineage.
func LoadLineageExpr() string {
	return "loadLineage($current_world_id, $current_checkpoint_id)"
}
