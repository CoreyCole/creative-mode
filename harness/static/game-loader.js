// Bridge postMessage from game iframe to harness overlay.
// Game sends: { type: 'cursor-locked' } when mouse is captured,
//             { type: 'cursor-unlocked' } when Tab releases mouse.
// We click hidden trigger buttons whose data-on:click sets Datastar signals.
window.addEventListener('message', function(event) {
    if (!event.data || !event.data.type) return;

    if (event.data.type === 'cursor-locked') {
        var el = document.getElementById('game-cursor-lock-trigger');
        if (el) el.click();
    } else if (event.data.type === 'cursor-unlocked') {
        var el = document.getElementById('game-cursor-unlock-trigger');
        if (el) el.click();
    } else if (event.data.type === 'navigate-world') {
        // 2D world navigation via postMessage from Bevy WASM
        window.location.href = '/world/' + event.data.data;
    } else if (event.data.type === 'navigate-checkpoint') {
        var parts = event.data.data.split('/');
        window.loadCheckpoint(parts[0], parts[1]);
    } else if (event.data.type === 'open-embed') {
        // Future: open YouTube/iframe overlay
        console.log('open-embed:', event.data.data);
    }
});

// Focus the game iframe when overlay closes via Tab.
// Pointer lock requires a user gesture from within the iframe itself,
// so we can't auto-lock the cursor. But by focusing the iframe, the
// user's next click or Tab press goes directly to the game.
window.focusGameFrame = function() {
    var frame = document.getElementById('game-frame');
    if (frame) frame.focus();
};

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
