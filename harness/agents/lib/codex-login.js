#!/usr/bin/env node
/**
 * Interactive Codex OAuth login.
 *
 * Usage: node harness/agents/lib/codex-login.js
 *
 * Opens an OAuth flow. On a headless server, copy the printed URL
 * to your local browser, complete login, then paste the redirect URL
 * back into this terminal.
 *
 * Tokens are saved to ~/.config/pi-codex/tokens.json
 */
import { writeFileSync, mkdirSync } from 'node:fs';
import { homedir } from 'node:os';
import { join } from 'node:path';
import { createInterface } from 'node:readline';
import { loginOpenAICodex } from '@mariozechner/pi-ai/dist/utils/oauth/openai-codex.js';

const TOKEN_DIR = join(homedir(), '.config', 'pi-codex');
const TOKEN_FILE = join(TOKEN_DIR, 'tokens.json');

function prompt(message) {
  const rl = createInterface({ input: process.stdin, output: process.stderr });
  return new Promise((resolve) => {
    rl.question(message, (answer) => {
      rl.close();
      resolve(answer);
    });
  });
}

console.error('\n=== OpenAI Codex OAuth Login ===\n');

try {
  const result = await loginOpenAICodex({
    onAuth: ({ url }) => {
      console.error('Open this URL in your browser:\n');
      console.error(url);
      console.error(
        '\nAfter logging in, the browser will redirect to localhost:1455.'
      );
      console.error(
        'If that fails (headless server), copy the full redirect URL and paste it below.\n'
      );
    },
    onPrompt: async ({ message }) => {
      return prompt(message + ' ');
    },
    onProgress: (msg) => {
      console.error('[progress]', msg);
    },
  });

  const tokens = {
    access: result.access,
    refresh: result.refresh,
    expires: result.expires,
    accountId: result.accountId,
  };

  mkdirSync(TOKEN_DIR, { recursive: true });
  writeFileSync(TOKEN_FILE, JSON.stringify(tokens, null, 2), { mode: 0o600 });

  console.error(`\nTokens saved to ${TOKEN_FILE}`);
  console.error(`Account ID: ${result.accountId}`);
  console.error(
    `Expires: ${new Date(result.expires).toISOString()}`
  );
  console.error('\nCodex OAuth login complete. Agents will auto-refresh tokens.\n');
} catch (err) {
  console.error('\nLogin failed:', err.message);
  process.exit(1);
}
