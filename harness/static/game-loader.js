// loadCheckpoint navigates to a checkpoint's world page.
window.loadCheckpoint = function(worldID, checkpointID) {
    window.location.href = '/world/' + worldID;
};

// loadLineage fetches the checkpoint ancestry and renders it into the lineage view.
// Called from data-on:click="loadLineage($current_world_id, $current_checkpoint_id)"
// so signal values are passed as arguments (not read from stale DOM attributes).
window.loadLineage = function(worldID, cpID) {
    if (!worldID || !cpID) return;

    fetch('/world/' + worldID + '/lineage/' + cpID)
        .then(function(r) { return r.text(); })
        .then(function(html) {
            document.getElementById('lineage-view').innerHTML = html;
        });
};
