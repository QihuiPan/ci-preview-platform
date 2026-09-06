# Security Policy

## Supported versions

Security fixes are provided for the latest tagged minor release.

## Reporting a vulnerability

Do not open a public issue for a suspected vulnerability. Use GitHub private vulnerability reporting for this repository and include:

- the affected version or commit;
- the attack prerequisites and impact;
- a minimal reproduction that does not contain real secrets;
- any suggested mitigation.

Expect an acknowledgement within five business days. No service-level commitment is implied for this portfolio project.

## Deployment warning

Version 0.1.0 is a reference implementation, not an internet-ready hosted CI service. Before production use, add durable PostgreSQL transactions, workload identity, worker API authentication, secret brokering, object-store authorization, rate limiting, audit persistence, and real Kubernetes isolation. Review [`docs/threat-model.md`](docs/threat-model.md) and [`docs/known-limitations.md`](docs/known-limitations.md).
