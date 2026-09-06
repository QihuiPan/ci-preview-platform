# Preview Cleanup Runbook

## Trigger

Use this runbook when a pull request is closed but its preview remains exposed past the reconciliation objective, or when preview deletion counters stop moving.

## Immediate checks

1. Replay the original close delivery only if its signature and delivery ID are preserved; deduplication makes this safe.
2. Read the preview record and confirm `desired_state` is `DELETING`.
3. Confirm no later open or synchronize event was accepted for a different pull-request generation.
4. Verify exposure is revoked before deleting workload resources.
5. Identify all resources by repository, pull-request, generation, and owner labels.

## Safe recovery

In version 0.1.0, wait for `PREVIEW_DELETE_DELAY` and the next one-second reconciliation tick. In a GitOps deployment, remove the desired manifest, force an Argo CD refresh, and delete only resources that match the preview owner labels. Do not delete an unlabelled namespace or a namespace whose repository and pull-request labels do not match the record.

Retain the deletion tombstone until DNS, ingress, namespace, and owned artefacts are absent. This prevents a delayed worker completion from recreating the preview.
