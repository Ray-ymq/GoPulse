# Phase 20-03 optimization verification

Phase 20-03 is frozen as `verify_only`. Phase 20-01 completed its twelve-cell
capacity baseline without an unresolved product cause, and Phase 20-02
completed the bounded trace/freshness chain while its dependency-delay and
Collector-outage cases remained controlled acceptance conditions. The batch
therefore re-runs the `2.2.2` B0 candidate without creating B1 or claiming a
performance improvement.

`loadtest/phase20-optimization-profile.json` binds the original Phase 20
recipe, workload mix, 50/100/150/200 RPS stages, three repetitions, recovery
semantics, resource sampling, and the Phase 20 Trace overlay. The runner
first creates an immutable contract containing the B0 source revision,
self-built image IDs, dependency digests, candidate manifest digest, and tool
digests. Every formal cell then uses an owned empty Compose project and
completes the same stop, drain, independent recovery, evidence, and cleanup
sequence used by the Phase 20 diagnostic runner.

The raw `optimization.json`, `diagnostic.json`, load ledgers, snapshots,
resource samples, recovery observations, lifecycle receipts, and candidate
manifest remain private execution evidence. The strict verifier recalculates
the twelve isolated cells and accepts
`optimization_status=not_needed` only when the B0 candidate is complete, all
correctness and non-degradation gates pass, no B1 or improvement field is
present, and the candidate/tool/artifact bindings remain unchanged.

The applicable fixed self-test cases are O01 (isolated repetition and stage
identity), O02 (no fabricated improvement calculation), O03 (verify-only
rejects B1), and O05 (correctness or non-degradation failure blocks
completion). O04 is explicitly not applicable because this mode has no
experimental product change to withdraw.

The frozen contract is created once, followed by a new preflight directory and
a new formal directory. The formal command defaults to the twelve-cell mode:

```bash
scripts/verify-phase20-optimization.sh --init-contract --contract "$private_root/contract.json"
scripts/verify-phase20-optimization.sh --preflight --contract "$private_root/contract.json" --work "$private_root/preflight-1"
scripts/verify-phase20-optimization.sh --contract "$private_root/contract.json" --work "$private_root/formal-1"
python3 scripts/verify-phase20-evidence.py --optimization "$private_root/formal-1"
```

The observer requires `kafka-python==2.2.15`, `python-snappy==0.7.3`, and
`cramjam==2.11.0`; install them outside the repository and expose their directory
through `PYTHONPATH` if they are not installed in the active Python environment.
Work directories must not already exist. Contracts and work directories are
never overwritten to repair a failed run.

Contract initialization is a batch operation performed before completion,
while root `VERSION` is still `2.2.2`. Replaying the experiment requires the
same B0 product checkout and frozen acceptance tools; initialization from a
completed `2.2.3` checkout intentionally fails the B0 version gate.

The tested product is the `2.2.2` B0 revision; completion metadata advances to
`2.2.3` only after all gates pass. The final source and behavior configuration
must match B0, apart from the registered version metadata. This batch does not
certify an independently rebuilt `2.2.3` image or a sustained-run window.

<!-- phase20-03-verified-results -->

## Verified batch result

`execution_status=complete`, `capability_status=target_met`, and
`optimization_status=not_needed`. All twelve isolated B0 cells passed;
B1 and improvement rate are `not_applicable`.

The contract digest is `sha256:190405c1c6c2b72656d6699afcc5772b6cd2e0284be4431a44682bc92d8a1635`.
The candidate manifest digest is `sha256:5740b644be10441336ae0ad025b5acc7fae0918f8c78e31e8c21ac0ceef97cc0`.

Values below are ordered repeat 1/2/3. CV is the population standard
deviation divided by the absolute mean, expressed as a percentage; zero
means use CV=0. Values and aggregates are descriptive repeatability results.

Resource columns are explicitly per-cell sampled peaks/minima: CPU and RSS
sum all owned containers (including dependencies and Collector); CPU can
exceed 100% across cores. Kafka lag and RabbitMQ counts sum observed
partitions/queues. Host free disk includes private acceptance evidence and
other host activity; Elasticsearch growth covers the owned observability
store over this finite cell window. These costs are not optimization gains.

Full request ledgers, resource samples, snapshots, recovery probes and
identity-bearing chain evidence remain private. The four recovery values
are first successful observations from their independent origins, not
a production freshness SLO or a sustained-run claim.

### rps-50

| Metric | Raw repeats 1 / 2 / 3 | Median | Min | Max | CV % |
| --- | --- | --- | --- | --- | --- |
| achieved_rps | 50 / 50 / 50 | 50 | 50 | 50 | 0.0 |
| p95_ms | 12.770021 / 16.823347 / 16.580151 | 16.580151 | 12.770021 | 16.823347 | 12.059455569753927 |
| p99_ms | 44.499776 / 44.962608 / 45.228125 | 44.962608 | 44.499776 | 45.228125 | 0.6703419896934195 |
| drain_seconds | 0.006141987 / 0.011318944 / 0.011719511 | 0.011318944 | 0.006141987 | 0.011719511 | 26.114665337850223 |
| schedule_lag_ms | 3.740579 / 3.016749 / 2.369913 | 3.016749 | 2.369913 | 3.740579 | 18.402042311578075 |
| requests | 3000 / 3000 / 3000 | 3000 | 3000 | 3000 | 0.0 |
| errors | 0 / 0 / 0 | 0 | 0 | 0 | 0 |
| dropped_slots | 0 / 0 / 0 | 0 | 0 | 0 | 0 |
| cpu_sum_peak_percent | 775.61 / 794.09 / 799.46 | 794.09 | 775.61 | 799.46 | 1.293533462224463 |
| rss_sum_peak_mib | 4011.3999910354614 / 3932.1579904556274 / 3946.5719900131226 | 3946.5719900131226 | 3932.1579904556274 | 4011.3999910354614 | 0.8695543268745424 |
| host_disk_min_gib | 99.04646682739258 / 98.4212760925293 / 97.80576705932617 | 98.4212760925293 | 97.80576705932617 | 99.04646682739258 | 0.5146266251797107 |
| es_store_growth_bytes | 4493616 / 2890325 / 2899169 | 2899169 | 2890325 | 4493616 | 21.989146938480687 |
| kafka_lag_sum_peak | 2 / 7 / 13 | 7 | 2 | 13 | 61.32153437832747 |
| rabbit_ready_sum_peak | 0 / 0 / 0 | 0 | 0 | 0 | 0 |
| rabbit_unacked_sum_peak | 0 / 1 / 0 | 0 | 0 | 1 | 141.4213562373095 |
| business_recovery_seconds | 8.604315981889158 / 13.368161881740889 / 4.8166571082856535 | 8.604315981889158 | 4.8166571082856535 | 13.368161881740889 | 39.18056201179812 |
| metrics_recovery_seconds | 6.939432393999596 / 12.779465199999322 / 4.164040145000399 | 6.939432393999596 | 4.164040145000399 | 12.779465199999322 | 45.103008427115945 |
| logs_recovery_seconds | 5.0632118199991965 / 9.874489273001018 / 1.246331697999267 | 5.0632118199991965 | 1.246331697999267 | 9.874489273001018 | 65.43893968695278 |
| events_recovery_seconds | 5.0491274270007125 / 9.861372753999603 / 1.230817219000528 | 5.0491274270007125 | 1.230817219000528 | 9.861372753999603 | 65.63013363168156 |

### rps-100

| Metric | Raw repeats 1 / 2 / 3 | Median | Min | Max | CV % |
| --- | --- | --- | --- | --- | --- |
| achieved_rps | 100 / 100 / 100 | 100 | 100 | 100 | 0.0 |
| p95_ms | 14.369987 / 33.963093 / 13.396 | 14.369987 | 13.396 | 33.963093 | 46.04408904319308 |
| p99_ms | 45.336305 / 78.886292 / 44.178411 | 45.336305 | 44.178411 | 78.886292 | 28.673480556360076 |
| drain_seconds | 0.004827728 / 0.002607656 / 0.003128502 | 0.003128502 | 0.002607656 | 0.004827728 | 26.920302150917287 |
| schedule_lag_ms | 5.183418 / 34.708286 / 3.817776 | 5.183418 | 3.817776 | 34.708286 | 97.8113963606075 |
| requests | 6000 / 6000 / 6000 | 6000 | 6000 | 6000 | 0.0 |
| errors | 0 / 0 / 0 | 0 | 0 | 0 | 0 |
| dropped_slots | 0 / 0 / 0 | 0 | 0 | 0 | 0 |
| cpu_sum_peak_percent | 796.42 / 799.59 / 778.25 | 796.42 | 778.25 | 799.59 | 1.1880023780657027 |
| rss_sum_peak_mib | 4264.327989578247 / 4240.287990570068 / 4253.787990570068 | 4253.787990570068 | 4240.287990570068 | 4264.327989578247 | 0.23135469602334033 |
| host_disk_min_gib | 98.86743927001953 / 98.24559783935547 / 97.6253547668457 | 98.24559783935547 | 97.6253547668457 | 98.86743927001953 | 0.5161312834953963 |
| es_store_growth_bytes | 4228228 / 5374184 / 5171419 | 5171419 | 4228228 | 5374184 | 10.13940115492447 |
| kafka_lag_sum_peak | 44 / 1144 / 47 | 47 | 44 | 1144 | 125.79092657895492 |
| rabbit_ready_sum_peak | 0 / 0 / 0 | 0 | 0 | 0 | 0 |
| rabbit_unacked_sum_peak | 1 / 1 / 1 | 1 | 1 | 1 | 0.0 |
| business_recovery_seconds | 7.435005308912878 / 6.329604255688537 / 5.559891421473367 | 6.329604255688537 | 5.559891421473367 | 7.435005308912878 | 11.947374713693018 |
| metrics_recovery_seconds | 1.4449695809998957 / 4.546836764000545 / 0.35684377899997344 | 1.4449695809998957 | 0.35684377899997344 | 4.546836764000545 | 83.8849922508362 |
| logs_recovery_seconds | 0.5479208209999342 / 1.6308768349990714 / 0.45324801900096645 | 0.5479208209999342 | 0.45324801900096645 | 1.6308768349990714 | 60.890840422228834 |
| events_recovery_seconds | 0.533338408000418 / 1.6057433519999904 / 1.4508699260004505 | 1.4508699260004505 | 0.533338408000418 | 1.6057433519999904 | 39.549967289814184 |

### rps-150

| Metric | Raw repeats 1 / 2 / 3 | Median | Min | Max | CV % |
| --- | --- | --- | --- | --- | --- |
| achieved_rps | 150 / 150 / 150 | 150 | 150 | 150 | 0.0 |
| p95_ms | 43.424653 / 35.713797 / 40.23004 | 40.23004 | 35.713797 | 43.424653 | 7.950136610206935 |
| p99_ms | 128.658103 / 117.525613 / 118.407476 | 118.407476 | 117.525613 | 128.658103 | 4.157717855624678 |
| drain_seconds | 0.002711703 / 0.002930287 / 0.003396793 | 0.002930287 | 0.002711703 | 0.003396793 | 9.483340642823407 |
| schedule_lag_ms | 15.683935 / 17.561732 / 46.275917 | 17.561732 | 15.683935 | 46.275917 | 52.81435084305113 |
| requests | 9000 / 9000 / 9000 | 9000 | 9000 | 9000 | 0.0 |
| errors | 0 / 0 / 0 | 0 | 0 | 0 | 0 |
| dropped_slots | 0 / 0 / 0 | 0 | 0 | 0 | 0 |
| cpu_sum_peak_percent | 795.75 / 795.68 / 770.35 | 795.68 | 770.35 | 795.75 | 1.5188387342762435 |
| rss_sum_peak_mib | 4315.3629903793335 / 4462.235990524292 / 4388.234991073608 | 4388.234991073608 | 4315.3629903793335 | 4462.235990524292 | 1.3662919154242252 |
| host_disk_min_gib | 98.68848419189453 / 98.07297134399414 / 97.45484161376953 | 98.07297134399414 | 97.45484161376953 | 98.68848419189453 | 0.5135332639420137 |
| es_store_growth_bytes | 13677487 / 7606988 / 5087509 | 7606988 | 5087509 | 13677487 | 41.01332562256336 |
| kafka_lag_sum_peak | 5403 / 4887 / 4624 | 4887 | 4624 | 5403 | 6.50867552859545 |
| rabbit_ready_sum_peak | 0 / 0 / 0 | 0 | 0 | 0 | 0 |
| rabbit_unacked_sum_peak | 2 / 2 / 1 | 2 | 1 | 2 | 28.284271247461902 |
| business_recovery_seconds | 10.431092610982887 / 8.43790314601938 / 10.058429596978385 | 10.058429596978385 | 8.43790314601938 | 10.431092610982887 | 8.97323181811831 |
| metrics_recovery_seconds | 23.96092195700112 / 28.981065445999775 / 21.887837807000324 | 23.96092195700112 | 21.887837807000324 | 28.981065445999775 | 11.938868443784397 |
| logs_recovery_seconds | 18.160035586999584 / 21.095519069000147 / 22.099848734998886 | 21.095519069000147 | 18.160035586999584 | 22.099848734998886 | 8.173306377686934 |
| events_recovery_seconds | 29.19027104000088 / 28.016492345999723 / 21.11974582000039 | 28.016492345999723 | 21.11974582000039 | 29.19027104000088 | 13.636057993612747 |

### rps-200

| Metric | Raw repeats 1 / 2 / 3 | Median | Min | Max | CV % |
| --- | --- | --- | --- | --- | --- |
| achieved_rps | 200 / 200 / 200 | 200 | 200 | 200 | 0.0 |
| p95_ms | 40.52211 / 46.213877 / 46.669917 | 46.213877 | 40.52211 | 46.669917 | 6.289417146178857 |
| p99_ms | 148.581729 / 147.8426 / 143.562242 | 147.8426 | 143.562242 | 148.581729 | 1.5086821016697696 |
| drain_seconds | 0.001526548 / 0.001778353 / 0.001728872 | 0.001728872 | 0.001526548 | 0.001778353 | 6.491871187040628 |
| schedule_lag_ms | 16.269807 / 38.919663 / 15.556566 | 16.269807 | 15.556566 | 38.919663 | 46.00652888263323 |
| requests | 12000 / 12000 / 12000 | 12000 | 12000 | 12000 | 0.0 |
| errors | 0 / 0 / 0 | 0 | 0 | 0 | 0 |
| dropped_slots | 0 / 0 / 0 | 0 | 0 | 0 | 0 |
| cpu_sum_peak_percent | 806.6 / 800.29 / 788.2 | 800.29 | 788.2 | 806.6 | 0.9562450656788809 |
| rss_sum_peak_mib | 4335.409989356995 / 4319.876990318298 / 4415.138989448547 | 4335.409989356995 | 4319.876990318298 | 4415.138989448547 | 0.9578203345593497 |
| host_disk_min_gib | 98.51448440551758 / 97.89986419677734 / 97.28092956542969 | 97.89986419677734 | 97.28092956542969 | 98.51448440551758 | 0.5144083533167236 |
| es_store_growth_bytes | 8358019 / 7294903 / 8102962 | 8102962 | 7294903 | 8358019 | 5.722775380199327 |
| kafka_lag_sum_peak | 7268 / 7028 / 7177 | 7177 | 7028 | 7268 | 1.3821362561065034 |
| rabbit_ready_sum_peak | 0 / 0 / 0 | 0 | 0 | 0 | 0 |
| rabbit_unacked_sum_peak | 2 / 2 / 1 | 2 | 1 | 2 | 28.284271247461902 |
| business_recovery_seconds | 12.125046109638788 / 9.481394225334952 / 9.85166298360491 | 9.85166298360491 | 9.481394225334952 | 12.125046109638788 | 11.145992638571212 |
| metrics_recovery_seconds | 38.456133828998645 / 40.26965483800086 / 37.15904706400033 | 38.456133828998645 | 37.15904706400033 | 40.26965483800086 | 3.3025568310344715 |
| logs_recovery_seconds | 35.79519518000052 / 38.42333952700028 / 34.54208178299996 | 35.79519518000052 | 34.54208178299996 | 38.42333952700028 | 4.461144648458172 |
| events_recovery_seconds | 40.73648422000042 / 22.266431763000583 / 34.52005185099915 | 34.52005185099915 | 22.266431763000583 | 40.73648422000042 | 23.605089550561793 |
