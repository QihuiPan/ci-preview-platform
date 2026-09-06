# Webhook replay

1. Verify App installation ID, repository allowlist, configured webhook secret, HTTPS endpoint and App permissions.
2. Find the original delivery in GitHub's App delivery history. Keep its delivery ID and signed body unchanged.
3. Use GitHub's Redeliver action after fixing connectivity or configuration. A duplicate accepted delivery returns its existing logical result.
4. A 401 means signature verification failed; 403 means repository/installation policy rejected it; 422 means the immutable revision has invalid or missing configuration; 429 means admission is full; 503 means a dependency is unavailable.
5. Do not invent a new delivery ID to override an old body. A new revision should produce a new event and configuration digest.
6. For out-of-order PR events, the stored updated_at clock and close tombstone win. Replaying an old head is intentionally ignored.

Never log the App private key, installation token, webhook signing secret, or full Authorization header while troubleshooting.
