# Service Level Objectives and Error Budget Policy

## Scope

- Service: `sre-mini`
- Measurement source: Prometheus metrics from `/metrics`
- Evaluation window: rolling 30 days

## SLOs

### 1) Availability SLO

- Target: `99.90%` successful requests over 30 days.
- Success definition: HTTP status code is not `5xx`.
- Error budget: `0.10%` of requests may fail.

PromQL SLI (availability):

```promql
1 - (
  sum(rate(http_requests_total{code=~"5.*"}[5m]))
  /
  clamp_min(sum(rate(http_requests_total[5m])), 0.001)
)
```

### 2) Latency SLO (`/work`)

- Target: p95 latency `< 300ms`.
- Measurement: histogram quantile over `http_request_duration_seconds_bucket`.

PromQL SLI (latency):

```promql
histogram_quantile(
  0.95,
  sum by (le) (rate(http_request_duration_seconds_bucket{path="/work"}[5m]))
)
```

## Error budget policy

For a 30-day window at 99.90% availability:
- Allowed unavailability: `0.10%`
- Equivalent downtime budget: `43m 12s`

Burn-rate policy:

- `SREMiniErrorBudgetBurnFast` (critical):
  - 5m and 1h windows exceed `14.4x` burn.
  - Action: declare `SEV-1`, pause risky releases, prioritize restoration.

- `SREMiniErrorBudgetBurnSlow` (warning):
  - 30m and 6h windows exceed `6x` burn.
  - Action: declare `SEV-2`, tighten release controls, prioritize mitigations.

## Release governance tied to budget consumption

- `< 25%` budget used: normal release velocity.
- `25% - 50%` used: require heightened review for risky changes.
- `50% - 100%` used: freeze non-critical releases until trend improves.
- `> 100%` used: freeze all non-emergency changes; reliability remediation only.

## Review cadence

- Weekly reliability review:
  - SLO attainment,
  - burn-rate trends,
  - unresolved corrective actions.
- Monthly:
  - reassess targets,
  - tune alerts,
  - update policy exceptions.
