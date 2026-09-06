# Webhook Replay Runbook

## Purpose

Webhook replay is safe only when the original delivery ID and body are preserved. A new delivery ID represents new intent and can create a new pipeline.

## Procedure

1. Confirm the stored event type, repository, delivery ID, body digest, and receive time.
2. Confirm the event body contains no credentials before copying it to a controlled test location.
3. Recompute `X-Hub-Signature-256` with the active webhook secret.
4. Send the original event name, delivery ID, and exact body bytes.
5. Expect `200 OK` with `duplicate: true` when the original delivery was accepted.
6. Verify no additional pipeline, jobs, or attempts were created.

Do not change whitespace in the body after calculating the HMAC. Do not log the webhook secret or paste it into a shell history shared with other users.
