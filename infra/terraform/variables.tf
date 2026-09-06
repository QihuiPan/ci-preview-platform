variable "namespace" {
  description = "Namespace for the CI control plane."
  type        = string
  default     = "ci-preview-system"
}

variable "image_repository" {
  description = "OCI repository for the control-plane image."
  type        = string
  default     = "ghcr.io/qihuipan/ci-preview-platform"
}

variable "image_tag" {
  description = "Immutable control-plane image tag or digest."
  type        = string
  default     = "0.1.0"
}

variable "github_webhook_secret" {
  description = "GitHub App webhook HMAC secret."
  type        = string
  sensitive   = true
}
