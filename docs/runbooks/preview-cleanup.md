# Preview cleanup

1. Use cictl previews to inspect desired state, observed state, pipeline owner, generation, expiration and last error.
2. A PR close or TTL expiry requests DELETING. The controller deletes the owned namespace and reports DELETED only once it is absent.
3. Check controller logs, Kubernetes events, ingress ownership and namespace finalizers if deletion does not converge. The controller must retain RBAC permissions during cleanup.
4. Do not relabel a foreign namespace to make ownership checks pass. Investigate a collision or unauthorized creation.
5. Verify the service, ingress and namespace are gone. Public DNS may remain wildcard-configured; it should no longer route to the removed service.
6. The durable PR clock survives resource removal. Replaying an older open/synchronize event must not recreate the preview.

Before uninstalling the controller, allow dynamic namespaces to clean up. Uninstalling Helm alone does not remove resources created at runtime.
