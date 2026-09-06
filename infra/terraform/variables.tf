variable "namespace" {
  description = "Existing namespace containing the application Secret."
  type        = string
  default     = "ci-platform"
}
variable "release_name" {
  description = "Release name matching the database-url in the existing Secret."
  type        = string
  default     = "ci"
}
variable "image" {
  description = "Pullable platform image built from the intended release."
  type        = string
}
variable "existing_secret" {
  description = "Existing complete application Secret; not managed in Terraform state."
  type        = string
  default     = "ci-secrets"
}
variable "minio_image" {
  description = "Pullable MinIO image built with the release Dockerfile.minio."
  type        = string
}
