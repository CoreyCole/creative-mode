import { createInterface } from 'readline';

let rl;
let stdinClosed = false;

export function initProtocol() {
  rl = createInterface({ input: process.stdin, terminal: false });
  rl.on('close', () => { stdinClosed = true; });
}

export function readLine() {
  return new Promise((resolve, reject) => {
    if (stdinClosed) {
      reject(new Error('stdin closed — orchestrator exited'));
      return;
    }
    const onLine = (line) => {
      rl.removeListener('close', onClose);
      resolve(JSON.parse(line));
    };
    const onClose = () => {
      rl.removeListener('line', onLine);
      reject(new Error('stdin closed — orchestrator exited'));
    };
    rl.once('line', onLine);
    rl.once('close', onClose);
  });
}

export function sendEvent(event, tool, data, toolCallId) {
  process.stdout.write(JSON.stringify({ type: 'event', event, tool, data, toolCallID: toolCallId }) + '\n');
}

export function sendQuestion(id, text) {
  process.stdout.write(JSON.stringify({ type: 'question', id, text }) + '\n');
}

export function sendResult(data) {
  process.stdout.write(JSON.stringify({ type: 'result', data }) + '\n');
}

// Periodic heartbeat so the Go orchestrator knows the agent is alive
// during long LLM generation pauses (no tool calls = no stdout output).
let heartbeatTimer = null;
const HEARTBEAT_INTERVAL_MS = 15_000;

export function startHeartbeat() {
  if (heartbeatTimer) return;
  heartbeatTimer = setInterval(() => {
    process.stdout.write(JSON.stringify({ type: 'heartbeat' }) + '\n');
  }, HEARTBEAT_INTERVAL_MS);
  // Don't keep the process alive just for heartbeats
  heartbeatTimer.unref();
}

export function stopHeartbeat() {
  if (heartbeatTimer) {
    clearInterval(heartbeatTimer);
    heartbeatTimer = null;
  }
}
