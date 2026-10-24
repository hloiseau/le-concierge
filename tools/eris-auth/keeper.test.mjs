import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, readFile, rm, stat } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { atomicWrite, maintain, refreshDue, usable } from './keeper.mjs';

const connected = () => ({ status: 'connected', scopes: ['chatgpt.tokens.use.direct'],
  credentials: { accessToken: 'a'.repeat(40), refreshToken: 'private', expiresAt: Date.now() + 60_000 } });

test('atomic replacement is visible through a directory mount and private on Unix', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'eris-auth-'));
  try {
    const path = join(dir, 'token');
    await atomicWrite(path, 'old'); await atomicWrite(path, 'new');
    assert.equal(await readFile(path, 'utf8'), 'new');
    if (process.platform !== 'win32') assert.equal((await stat(path)).mode & 0o777, 0o600);
  } finally { await rm(dir, { recursive: true }); }
});

test('expired, unverified, or unconsented tokens cannot be published', () => {
  const state = connected();
  assert.ok(usable(state));
  assert.ok(!usable({ ...state, scopes: [] }));
  assert.ok(!usable({ ...state, pendingRefresh: {} }));
  assert.ok(!usable({ ...state, status: 'reauth_required' }));
  assert.ok(!usable(state, state.credentials.expiresAt));
});

test('refresh respects earliest_refresh_at but resumes checkpoint immediately', () => {
  const state = connected();
  assert.ok(refreshDue(state));
  state.credentials.earliestRefreshAt = (Date.now() + 60_000) / 1000;
  assert.ok(!refreshDue(state));
  assert.ok(refreshDue({ ...state, pendingRefresh: {} }));
  assert.ok(!refreshDue({ ...state, pendingRefresh: {}, status: 'reauth_required' }));
});

test('rotation checkpoint survives verification outage without publishing', async () => {
  let saved, published = false, removed = false;
  const pending = { credentials: { accessToken: 'unverified' } };
  await assert.rejects(maintain(connected(), {
    refresh: async (_, signal, checkpoint) => { await checkpoint(pending); throw Object.assign(new Error(), { retryable: true }); },
    save: async value => { saved = value; }, publish: async () => { published = true; }, remove: async () => { removed = true; },
  }));
  assert.deepEqual(saved.pendingRefresh, pending);
  assert.equal(published, false); assert.equal(removed, true);
});

test('permanent refresh failure disables access and preserves registration', async () => {
  let saved, removed = false;
  await assert.rejects(maintain({ ...connected(), clientId: 'eris' }, {
    refresh: async () => { throw Object.assign(new Error(), { retryable: false }); },
    save: async value => { saved = value; }, publish: async () => assert.fail(), remove: async () => { removed = true; },
  }));
  assert.equal(saved.status, 'reauth_required'); assert.equal(saved.clientId, 'eris'); assert.ok(removed);
});

test('verified refresh is saved before exposing its access token', async () => {
  const calls = [];
  const next = connected(); next.credentials.expiresAt += 3600_000;
  await maintain(connected(), {
    refresh: async () => next, save: async () => calls.push('save'),
    publish: async token => { assert.equal(token, next.credentials.accessToken); calls.push('publish'); },
    remove: async () => assert.fail(),
  });
  assert.deepEqual(calls, ['save', 'publish']);
});
