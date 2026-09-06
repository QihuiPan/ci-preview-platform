# Fault Exercise Evidence

## Acceptance matrix

| Injected condition | Expected invariant | Automated evidence |
| --- | --- | --- |
| Duplicate webhook delivery | One logical pipeline and no duplicate jobs | `TestCreatePipelineForDeliveryIsIdempotent`, `TestGitHubWebhookVerifiesAndDeduplicates` |
| Cyclic or unknown dependency | Invalid pipeline is rejected before persistence | `TestValidateRejectsCycle`, `TestValidateRejectsUnknownDependency`, `TestManualPipelineRejectsCycle` |
| One tenant submits a burst | Other tenants continue receiving slots | `TestSchedulerRotatesAcrossTenants`, `TestHundredConcurrentJobsRemainFair` |
| Tenant reaches concurrent quota | Another eligible tenant can still schedule | `TestTenantQuotaDoesNotBlockAnotherTenant` |
| Trusted job sees mixed worker pools | Only a trusted worker receives the lease | `TestTrustedJobUsesTrustedWorker` |
| Worker stops renewing a lease | A new attempt is issued and old completion is rejected | `TestExpiredLeaseRetriesAndRejectsStaleCompletion` |
| Cancellation races completion | Pipeline and job converge on one terminal state | `TestCancelCompletionRaceHasOneTerminalOutcome` |
| Pull request closes during final work | Deletion tombstone blocks late preview activation | `TestClosedPreviewTombstonePreventsLateActivation` |
| Malicious cache entry uses traversal or excess size | Entry is rejected before extraction | `TestValidateArchiveEntry` |
| Full API-to-worker-to-preview path | Ordered attempts produce a final preview URL | `TestPipelineToPreviewFlow` and the manual process test in `test-report.md` |

## Reproduction

Run the deterministic fault suite with:

```bash
go test -count=100 ./internal/control ./internal/planner ./internal/cache
go test -race ./...
```

The high repetition count exercises scheduling and cancellation ordering. The Linux race detector checks that the store lock protects all shared state. Failure output must retain IDs and states but must not include lease tokens or secrets.
