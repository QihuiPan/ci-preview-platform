output "namespace" {
  description = "Deployed control-plane namespace."
  value       = kubernetes_namespace_v1.control_plane.metadata[0].name
}
