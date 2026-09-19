# Phase 17 Compose acceptance and Phase 18 handoff

Phase 17 closes the pre-Kubernetes engineering acceptance of the complete GoPulse
Compose product. The accepted product boundary is the immutable `1.14.5`
Linux `amd64` candidate; Kubernetes is not part of this acceptance.

## Accepted candidate

| Item | Value |
| --- | --- |
| Git revision | `7e87bca62b1f94904db3c3ec6ed26d01308e7d8c` |
| Release manifest | `sha256:ad377875d2855635a47c8664d48d906653a264ab1dff11ab0cf4ca260af11c84` |
| Bundle | `sha256:e76134207ef150f9c7f231ab119c87ecb89fe4479909808531ab77b52a445acc` |
| Runtime contract | `sha256:19f31816ec250e7cb2913dcb98e8ecc2e431c432cb984e234764e9d0acf64880` |
| Host/server | Linux WSL2 `x86_64`, Docker server `linux/amd64` 29.7.2 |
| Compose | Docker Compose v5.5.0 |
| Publication boundary | Loopback-only task registry; no external/public promotion |

The machine-verifiable evidence is stored under
`dev/logs/Phase-17/evidence/Phase-17-05-evidence/`. The private work directory,
credentials, passphrases, backup payloads, and execution journals are not
committed.

## Verified matrix

The final same-candidate matrix passed these scenarios:

- release artifact/runtime gate and complete Compose receipt reuse;
- runtime contract, probes, configuration negatives, request IDs, safe errors,
  structured logs, SIGTERM/SIGINT and bounded restart behavior;
- clean lifecycle and failure lifecycle with owned cleanup and isolation;
- direct `1.13.6` predecessor migration, repeat/current migration, dirty-state
  refusal, restore-after-failure and continued writes;
- RabbitMQ business delivery/retry/dead/reconnect/idempotency and Search Indexer
  recovery;
- Kafka Marshaller store-before-commit, retry/ownership/permanent-failure and
  replacement persistence;
- three-source alert evaluation, incident/audit continuity and recovery;
- complete business and observability Compose topology, both frontends, role
  boundaries, internal-route denial and six official plugins;
- current backup/inspect/restore regression, source/target API continuity,
  restored business/search/observability facts, secret scan and owned cleanup.

The authoritative final receipt reports all eleven Phase 17 scenarios as passed,
with candidate binding, redaction, cleanup and resource-isolation checks passed.

## Operations and Phase 18 handoff

Phase 18 may consume the following unchanged contracts:

- the Linux `amd64` Compose product and immutable release identity;
- `/startup`, `/live`, `/ready` and `/health` semantics;
- shared shutdown budgets and readiness withdrawal before drain;
- Migration schema target 13, dirty-state refusal and backup/restore boundary;
- RabbitMQ, Kafka and alert ownership/retry/idempotency behavior;
- dual frontend authentication/role boundaries and plugin supervision;
- the sanitized Phase 17 evidence schema and candidate-binding rules.

Moving the product to Kubernetes must change deployment location and orchestration,
not silently change these application contracts. Phase 17 does not claim macOS,
Windows, `linux/arm64`, Kubernetes, production HA, capacity, or public registry
support.
