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
