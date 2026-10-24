// Personal NAS deployment. OAuth and ID-token verification use the pinned OpenAI devkit.
import { randomUUID } from 'node:crypto';
import { mkdir, open, readFile, rename, rm } from 'node:fs/promises';
import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { setTimeout as delay } from 'node:timers/promises';

export async function atomicWrite(path, value) {
  const temporary = `${path}.${randomUUID()}.tmp`;
  const file = await open(temporary, 'wx', 0o600);
  try {
    await file.writeFile(value);
    await file.sync();
  } finally { await file.close(); }
  try { await rename(temporary, path); }
  finally { await rm(temporary, { force: true }); }
}

const sharing = 'chatgpt.tokens.use.direct';
export function usable(connection, now = Date.now()) {
  return connection?.status === 'connected' && !connection.pendingRefresh &&
    connection.scopes?.includes(sharing) &&
    typeof connection.credentials?.accessToken === 'string' &&
    connection.credentials.accessToken.length >= 32 &&
    connection.credentials.expiresAt > now + 15_000;
}

export function refreshDue(connection, now = Date.now()) {
  if (connection?.status !== 'connected') return false;
  if (connection?.pendingRefresh) return true;
  if (!connection.credentials) return false;
  const earliest = connection.credentials.earliestRefreshAt;
  const allowedAt = typeof earliest === 'number' ? earliest * 1000 : Date.parse(earliest ?? '');
  return connection.credentials.expiresAt <= now + 120_000 && (!Number.isFinite(allowedAt) || now >= allowedAt);
}

export async function maintain(connection, { refresh, save, publish, remove }, signal) {
  try {
    if (refreshDue(connection)) {
      connection = await refresh(connection, signal, async pendingRefresh => {
        connection = { ...connection, pendingRefresh };
        await remove();
        await save(connection);
      });
      await save(connection);
    }
  } catch (error) {
    // An unverified rotation is never published, including after a restart.
    if (error.retryable !== true) {
      connection = { ...connection, status: 'reauth_required' };
      await save(connection);
    }
    if (!usable(connection)) await remove();
    throw error;
  }
  if (usable(connection)) await publish(connection.credentials.accessToken);
  else await remove();
  return connection;
}

async function readJSON(path) {
  try { return JSON.parse(await readFile(path, 'utf8')); }
  catch (error) { if (error.code === 'ENOENT') return undefined; throw error; }
}

function safeError(error) {
  const known = new Set(['cancelled', 'access_denied', 'invalid_grant', 'invalid_refresh_token',
    'refresh_token_expired', 'refresh_token_invalidated', 'refresh_token_reused', 'invalid_id_token',
    'invalid_token_response', 'account_mismatch', 'identity_verification_unavailable',
    'discovery_failed', 'network_error', 'callback_port_unavailable', 'registration_incomplete',
    'subscription_sharing_user_not_eligible', 'subscription_sharing_usage_limit_exceeded']);
  return known.has(error.code) ? error.code : 'connection_unavailable';
}

async function main() {
  process.umask(0o077);
  const directory = process.env.ERIS_AUTH_STATE ?? '/state';
  const tokens = join(directory, 'tokens');
  const statePath = join(directory, 'connection.json');
  const tokenPath = join(tokens, 'access_token');
  const command = process.argv[2] ?? 'serve';
  if (command === 'health') {
    const connection = await readJSON(statePath);
    if (!usable(connection) || (await readFile(tokenPath, 'utf8')) !== connection.credentials.accessToken) process.exitCode = 1;
    return;
  }
  if (!['login', 'serve'].includes(command)) throw new Error('invalid command');
  await mkdir(directory, { recursive: true, mode: 0o700 });
  await mkdir(tokens, { recursive: true, mode: 0o700 });
  const { default: lockfile } = await import('proper-lockfile');
  const release = await lockfile.lock(directory, {
    lockfilePath: join(directory, '.keeper.lock'), stale: 120_000, update: 20_000, retries: 0,
  });
  const stop = new AbortController();
  process.once('SIGTERM', () => stop.abort());
  process.once('SIGINT', () => stop.abort());
  const sdk = process.env.ERIS_SIWC_MODULE ?? '/app/sdk/oauth.js';
  const { authorize, refreshConnection } = await import(pathToFileURL(resolve(sdk)).href);
  const save = connection => atomicWrite(statePath, JSON.stringify(connection));
  const remove = () => rm(tokenPath, { force: true });
  const publish = token => atomicWrite(tokenPath, token);
  try {
    if (command === 'login') {
      let host = await readJSON(join(directory, 'host.json'));
      if (!host) {
        host = { id: `urn:uuid:${randomUUID()}` };
        await atomicWrite(join(directory, 'host.json'), JSON.stringify(host));
      }
      const previous = await readJSON(statePath) ?? await readJSON(join(directory, 'registration.json'));
      const connection = await authorize({ appName: 'Eris', redirectPort: 14551, sendHostId: true,
        openBrowser: url => console.log(JSON.stringify({ authorizationURL: url })) },
      previous, host.id, AbortSignal.any([stop.signal, AbortSignal.timeout(600_000)]), {
        onRegistration: clientId => atomicWrite(join(directory, 'registration.json'), JSON.stringify({ clientId })),
      });
      await save(connection);
      if (!usable(connection)) { await remove(); throw new Error('sharing not granted'); }
      await publish(connection.credentials.accessToken);
      console.log(JSON.stringify({ connected: true, sharing: true }));
      return;
    }
    while (!stop.signal.aborted) {
      try {
        const connection = await readJSON(statePath);
        if (!connection) { await remove(); throw new Error('login required'); }
        await maintain(connection, { refresh: refreshConnection, save, publish, remove },
          AbortSignal.any([stop.signal, AbortSignal.timeout(45_000)]));
      } catch (error) {
        console.log(JSON.stringify({ authError: safeError(error) }));
      }
      await delay(30_000, undefined, { signal: stop.signal }).catch(() => {});
    }
  } finally { await release(); }
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  main().catch(error => { console.error(JSON.stringify({ authError: safeError(error) })); process.exitCode = 1; });
}
