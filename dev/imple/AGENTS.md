# Implementation Plan Rules

These instructions apply to implementation plans under `dev/imple/`. The repository-wide rules in
[AGENTS.md](../../AGENTS.md) remain authoritative; do not duplicate or weaken their time, branch,
version, acceptance, evidence, or completion constraints.

## Required Test and Acceptance Planning

Before creating or revising an implementation plan, read and apply
[Test and Acceptance Tool Rules](../../scripts/AGENTS.md). Each split plan must map changed
behavior to existing coverage, concrete gaps, the lowest effective test layer, reused tooling,
permitted file changes and fixed gates. New stage numbers do not justify new executors; additions
and retirements require the necessity and coverage evidence defined there. Preserve allocated
acceptance contracts until an explicit plan revision changes them.

## Required Execution Budget

Every new or revised split plan must contain the following concrete fields before execution:

| Field | Required content |
| --- | --- |
| Delivery and scope | One primary result, exact permitted files, dependencies and excluded follow-up work |
| Expected and maximum time | Total active minutes and cumulative ceiling; required long experiments identified separately |
| Stage budgets | Discovery/implementation, direct checks, build/setup, acceptance, publication and safe cleanup; totals fit the file ceiling |
| Fixed gates | Case IDs, known commands, expected results, measurement windows, evidence and failure classification |
| Experiment cost | Unit count multiplied by warmup/measurement/recovery, plus measured or bounded setup/cleanup cost |
| Diagnosis limits | Smallest reproduction, maximum attempts and time; first infrastructure failure stops the full matrix |
| Evidence continuation | Passed/failed/not-started inventory, candidate/config/tool identity, actual supported recovery granularity and invalidation rules |
| Stop and completion | Progress checkpoints, remaining-budget check, safe stop, explicit resume instruction and exact completion conditions |

- Use the root rule's ordinary-file budget. A long-duration exception requires an irreducible-time
  calculation and a finite total ceiling; "until it passes" is not an estimate or a stop condition.
- Distinguish implementation, nonformal probes and formal acceptance. Do not hide full acceptance
  tool development inside an unrelated product or resource-budget plan.
- Mark unknown command interfaces as planned, and require their implementation before the dependent
  batch starts. Do not promise resume at a finer granularity than the actual tool supports.
- After a repair, list affected gates and still-valid checks before starting a rebuild or rerun.
  New candidates do not reset cumulative cost or allow old receipts to be rebound.
- On an explicit user stop, record the existing checkpoint without declaring success or discarding
  evidence. Planning revisions are not an instruction to resume execution.

The cost ledger and continuation procedure are described in
[Implementation execution budget](../rules/implementation-execution-budget.md).
