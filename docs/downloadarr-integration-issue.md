# Expose a verified personal integration contract for Concierge / Hub

Repository: `hloiseau/LibraryDownloadarr`, branch `codex/download-quality-a6fa824141dc`.

Related: https://github.com/hloiseau/le-concierge/issues/24 and https://github.com/hloiseau/le-hub/issues/15.

Published contract and implementation: [draft pull request #1](https://github.com/hloiseau/LibraryDownloadarr/pull/1). The repository has Issues disabled (GitHub 410), so the pull request carries this contract. It has not been merged or deployed.

The fork already implements personal conversion jobs, quality/audio/subtitle options, bounded queue, real progress, reuse and single-use download tickets. Concierge should reuse these endpoints. The missing prerequisite is a versioned identity/session contract: `/api/auth/me` exposes only a local user ID/name/admin flag, and `/api/health` exposes no build revision. A separate Downloadarr Plex login cannot currently be proven to belong to the same numeric Plex account and configured server as the Hub session. Matching usernames/emails or sharing administrator tokens would be unsafe.

Add a read-only, `Cache-Control: no-store` endpoint `GET /api/integrations/concierge`, authenticated with a separately issued Downloadarr personal bearer session:

```json
{
  "contractVersion": 1,
  "buildRevision": null,
  "user": { "plexId": "12345" },
  "plexServerId": "configured-machine-identifier",
  "sessionExpiresAt": 1792159200000,
  "capabilities": {
    "qualityOptions": ["720p-2", "720p-4", "1080p-8"],
    "streamOptions": true,
    "preparedDownloads": true,
    "singleUseTickets": true,
    "queuePersistence": "memory"
  }
}
```

- `plexId` is the verified positive numeric account ID already stored as `plex:<id>` by the fork; never derive identity from name/email or accept it from the request.
- Reject local administrator-only sessions and legacy unverified Plex identities with 403. A Plex-authenticated user who also has administrator privileges still uses their personal Plex identity/token.
- Derive server ID from current configuration; reject unconfigured instances. Revalidate the Plex account's access to that exact server with the personal token before returning the contract. Expose neither tokens nor server URLs.
- `sessionExpiresAt` is the real bearer session deadline in epoch milliseconds, not an arbitrary client TTL. Logout/expiration invalidates the endpoint.
- Quality options reflect the user's current download policy; forbidden users receive no usable integration grant. The response has no library names or other users' identifiers.
- `buildRevision` is a validated full Git revision supplied at image build time (null for local builds), also set as the OCI revision label. Do not claim that a branch name or package version alone identifies the deployed image.
- All job list/status responses must revalidate current Plex library/download policy before returning titles/status; ownership alone does not cover access revoked during a long conversion. Existing creation/reuse/transfer revalidation must remain intact.
- Document the existing contract's limitations: jobs are in memory, a restart loses their handles, and the Hub must not automatically recreate lost/expired conversions. Null progress remains indeterminate. File tickets remain one-use, 60-second, owner/session-bound POST bodies; never put bearer/Plex tokens in URLs.

Tests: anonymous, local admin, expired/logged-out session, wrong/unconfigured server, legacy identity, restricted/revoked library, distinct Plex accounts, policy-filtered quality options, no secret/URL leakage, valid and absent build revisions. No live conversion is required; use fixtures.

After that contract is tested and deployed, Concierge can start a distinct Downloadarr PIN flow server-side, compare the returned numeric Plex ID/server ID with its current grant, keep the Downloadarr bearer only in memory per Hub owner/session, and proxy allowlisted existing job endpoints. No Plex token exchange between the two services, no shared administrator session, no reimplementation of conversion.
