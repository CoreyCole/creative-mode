/**
 * OpenAI Codex OAuth token management.
 *
 * Reads tokens from ~/.codex/auth.json (shared with Codex CLI).
 * Auto-refreshes expired tokens using the refresh_token.
 */
import { readFileSync, writeFileSync } from 'node:fs';
import { homedir } from 'node:os';
import { join } from 'node:path';
import { refreshOpenAICodexToken } from '@mariozechner/pi-ai/dist/utils/oauth/openai-codex.js';

const CODEX_AUTH_FILE = process.env.CODEX_HOME
  ? join(process.env.CODEX_HOME, 'auth.json')
  : join(homedir(), '.codex', 'auth.json');

// Refresh 5 minutes before expiry
const EXPIRY_BUFFER_MS = 5 * 60 * 1000;
// Codex CLI tokens last ~1 hour from last_refresh
const TOKEN_LIFETIME_MS = 60 * 60 * 1000;

let cached = null;

function readCodexAuth() {
  try {
    const raw = JSON.parse(readFileSync(CODEX_AUTH_FILE, 'utf-8'));
    if (!raw.tokens?.access_token || !raw.tokens?.refresh_token) return null;

    // Infer expiry from last_refresh + 1 hour (Codex CLI doesn't store explicit expiry)
    let expires = Date.now() + TOKEN_LIFETIME_MS;
    if (raw.last_refresh) {
      expires = new Date(raw.last_refresh).getTime() + TOKEN_LIFETIME_MS;
    }

    return {
      access: raw.tokens.access_token,
      refresh: raw.tokens.refresh_token,
      accountId: raw.tokens.account_id,
      expires,
    };
  } catch {
    return null;
  }
}

function writeBackToCodexAuth(tokens) {
  try {
    const raw = JSON.parse(readFileSync(CODEX_AUTH_FILE, 'utf-8'));
    raw.tokens.access_token = tokens.access;
    raw.tokens.refresh_token = tokens.refresh;
    raw.last_refresh = new Date().toISOString();
    writeFileSync(CODEX_AUTH_FILE, JSON.stringify(raw, null, 2));
  } catch {
    // Best-effort — don't break the agent if write-back fails
  }
}

/**
 * Get a valid Codex access token, refreshing if needed.
 * Reads from ~/.codex/auth.json (shared with Codex CLI).
 */
export async function getCodexAccessToken() {
  if (!cached) {
    cached = readCodexAuth();
  }
  if (!cached) {
    throw new Error(
      `No Codex OAuth tokens found at ${CODEX_AUTH_FILE}. Run: codex auth login`
    );
  }

  // Still valid?
  if (Date.now() < cached.expires - EXPIRY_BUFFER_MS) {
    return cached.access;
  }

  // Refresh
  const refreshed = await refreshOpenAICodexToken(cached.refresh);
  cached = {
    access: refreshed.access,
    refresh: refreshed.refresh,
    expires: refreshed.expires,
    accountId: refreshed.accountId,
  };

  // Write back so Codex CLI also sees the fresh token
  writeBackToCodexAuth(cached);

  return cached.access;
}
