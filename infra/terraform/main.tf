# Optional chart wrapper. Provision the namespace and ci-secrets externally first.
# Avoid storing application credentials in Terraform state.
resource "helm_release" "platform" {
  name          = var.release_name
  namespace     = var.namespace
  chart         = "${path.module}/../../deploy/helm/ci-preview-platform"
  wait          = true
  wait_for_jobs = true
  timeout       = 600
  values = [yamlencode({
    image          = var.image
    existingSecret = var.existing_secret
    minio          = { image = var.minio_image }
  })]
}
