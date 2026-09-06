resource "kubernetes_namespace_v1" "control_plane" {
  metadata {
    name = var.namespace
    labels = {
      "pod-security.kubernetes.io/enforce" = "restricted"
    }
  }
}

resource "kubernetes_secret_v1" "webhook" {
  metadata {
    name      = "ci-preview-platform"
    namespace = kubernetes_namespace_v1.control_plane.metadata[0].name
  }
  data = {
    github-webhook-secret = var.github_webhook_secret
  }
}

resource "helm_release" "platform" {
  name      = "ci-preview-platform"
  namespace = kubernetes_namespace_v1.control_plane.metadata[0].name
  chart     = "../../deploy/helm/ci-preview-platform"

  set = [
    {
      name  = "image.repository"
      value = var.image_repository
    },
    {
      name  = "image.tag"
      value = var.image_tag
    }
  ]

  depends_on = [kubernetes_secret_v1.webhook]
}
