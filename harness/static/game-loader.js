// Bridge postMessage from game iframe to harness overlay.
// Game sends: { type: 'cursor-locked' } when mouse is captured,
//             { type: 'cursor-unlocked' } when backtick releases mouse.
// We click hidden trigger buttons whose data-on:click sets Datastar signals.
window.addEventListener('message', function(event) {
    if (!event.data || !event.data.type) return;

    if (event.data.type === 'cursor-locked') {
        var el = document.getElementById('game-cursor-lock-trigger');
        if (el) el.click();
    } else if (event.data.type === 'cursor-unlocked') {
        var el = document.getElementById('game-cursor-unlock-trigger');
        if (el) el.click();
    } else if (event.data.type === 'toggle-overlay') {
        var el = document.getElementById('game-overlay-toggle-trigger');
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

// Focus the game iframe when overlay closes via backtick.
// Pointer lock requires a user gesture from within the iframe itself,
// so we can't auto-lock the cursor. But by focusing the iframe, the
// user's next click goes directly to the game.
window.focusGameFrame = function() {
    var frame = document.getElementById('game-frame');
    if (frame) frame.focus();
};

// loadCheckpoint navigates to a checkpoint's world page.
// First updates the user's position via the checkpoint API, then navigates.
window.loadCheckpoint = function(worldID, checkpointID) {
    fetch('/world/' + worldID + '/checkpoint/' + checkpointID)
        .then(function() {
            window.location.href = '/world/' + worldID;
        })
        .catch(function() {
            // Fallback: navigate anyway (server will use whatever position is stored)
            window.location.href = '/world/' + worldID;
        });
};

// Upload asset from overlay form.
window.__uploadAsset = function() {
    var form = document.getElementById('upload-form');
    if (!form) return;
    var status = document.getElementById('upload-status');
    var data = new FormData(form);

    if (status) status.textContent = 'Uploading...';

    fetch('/api/assets/upload', { method: 'POST', body: data })
        .then(function(r) {
            if (!r.ok) throw new Error('Upload failed: ' + r.status);
            return r.json();
        })
        .then(function(result) {
            if (status) status.textContent = 'Uploaded: ' + (result.path || 'ok');
            form.reset();
        })
        .catch(function(err) {
            if (status) status.textContent = err.message;
        });
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
