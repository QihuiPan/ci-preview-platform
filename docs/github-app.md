# GitHub App integration

Manual public-repository builds work without an App. For automatic events and private repositories, create a GitHub App owned by your account or organization.

1. Set a public HTTPS webhook URL ending in /v1/webhooks/github. Use the randomly generated webhook-secret from the ci-secrets Secret as the webhook signing secret. Do not print or commit it.
2. Grant repository Contents: read, Metadata: read, Pull requests: read, and Checks: read/write. Subscribe to push and pull_request events. Install the App only on intended repositories.
3. Put the PEM private key into a Kubernetes Secret with key private-key.pem, then configure Helm github.appID and github.keySecret. Keep the downloaded private key outside the repository.
4. Update auth.json inside ci-secrets with a repository policy mapping the exact OWNER/REPO to tenant, trusted, allow_forks, installation_id, and optional image_prefix. Retain every existing credential and the state encryption key. Restart the API after changing policy.
5. Commit .ci-preview.yml to the target repository. Redeliver a signed event and inspect its response and the pipeline/check status.

Webhook payload installation_id must match the repository policy. The API fetches configuration at the immutable event SHA, not from the moving default branch. Private source checkout uses an installation token only in the git init container; command containers never receive it. Public forks run without checkout credentials and are rejected unless allow_forks is true.

An opened, reopened, or synchronize PR builds the current head; a closed event cancels active work and requests deletion. Other PR actions and deleted push refs are ignored. The PR updated_at clock fences older deliveries; close wins equal timestamps. Delivery deduplication prevents logical pipeline duplication. Invalid configuration returns 422 so the owner can fix and redeliver.

Checks are asynchronous. A successful pipeline means the jobs completed; preview status is available separately. GitHub downtime does not roll back an already committed pipeline. Check creation is at least once and can duplicate after a crash; inspect the pipeline external_id when reconciling checks.

This release includes the App adapter but does not create an App, choose your repositories' trust policy, or upload an App private key on your behalf. Those choices require account-owner setup.
