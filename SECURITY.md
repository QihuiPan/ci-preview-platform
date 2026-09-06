# Security policy

Report suspected vulnerabilities privately to the repository owner through an established private contact channel or GitHub private vulnerability reporting when available. Do not open a public issue containing secrets or exploit details.

Include the affected commit, prerequisites, impact, and a minimal reproduction with synthetic credentials. No response-time or service-level guarantee is implied.

This is a self-hosted small-team service, not an audited public multi-tenant CI platform. Read the threat model and deployment assumptions before accepting untrusted code. Use a dedicated execution cluster and an enforcing CNI. Never grant fork jobs trusted builder credentials.

Only the latest maintained release is intended to receive fixes. Operators remain responsible for rebuilding patched container images and updating supported dependencies.
