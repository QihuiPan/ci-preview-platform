# Optional Terraform wrapper

This wrapper manages the Helm release in an existing dedicated cluster. It does not create a cloud account, cluster, namespace, storage class, or application credentials. Follow the main installation guide to provision those prerequisites first. Do not manage the same release concurrently with standalone Helm commands.

Set `KUBE_CONFIG_PATH` to an explicit kubeconfig path and verify its selected context. See the [official Helm provider authentication documentation](https://registry.terraform.io/providers/hashicorp/helm/latest/docs).

```bash
cd infra/terraform
terraform init
terraform validate
terraform plan -var='image=YOUR_REGISTRY/ci-preview-platform:0.2.0' \
  -var='minio_image=YOUR_REGISTRY/ci-preview-minio:2025-10-15'
# Review the plan before applying it with the same variables.
```

Use `release_name` and `namespace` values that match the pre-created Secret's database URL. Credentials remain in the existing Kubernetes Secret and are not passed as Terraform values. Retained PVCs and dynamically created workload namespaces are not automatically deleted when destroying this wrapper. Read the operations guide before removing a deployment.
