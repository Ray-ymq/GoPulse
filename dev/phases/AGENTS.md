# Phase Planning Rules

These instructions apply to stage overviews and roadmaps under `dev/phases/`. Repository-wide
constraints come from [AGENTS.md](../../AGENTS.md); authoritative batch allocation and acceptance
remain in each Phase total implementation plan under `dev/imple/`.

- The sole global strategy is [GoPulse 全局架构与发展总纲](GoPulse-高并发与可观测后续路线图.md).
  `Plan.md` preserves allocated-stage history; overviews and indexes must follow the charter for
  future direction and must not change existing implementation contracts.

- State the business/engineering result of each batch, its dependency, and the evidence needed to
  unlock it. Keep capability completion, execution completeness and measured capacity separate.
- Estimate implementation and acceptance cost before proposing a split. Ordinary batches follow
  the root time budget; necessary long experiments have their own finite, visible cost.
- Place product changes, acceptance-tool development and long formal certification in separate
  batches when combining them would exceed the permitted scope or cost.
- Keep exact executable gates and per-stage budgets in the implementation plans, following
  [Implementation Plan Rules](../imple/AGENTS.md). Do not maintain a second set of numeric limits here.
- Do not reserve speculative future phases/versions or silently renumber existing branches.
  Recalculate only uncreated allocations through the authoritative total plan when a split changes.
- For an already-started batch, preserve actual progress and valid evidence. Describe remaining
  work from its real checkpoint; do not turn a planning update into a full restart.
- A paused or failed batch stays unfinished until its original or explicitly revised acceptance
  gates pass. Roadmaps must not treat documentation changes as implemented capabilities.
