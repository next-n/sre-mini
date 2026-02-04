## SRE Mini Incident Runbook

Use this playbook for live incident response triggered by alerts in `alerts/alerts.yaml`.

## Severity model

- `SEV-1`: customer-visible outage or rapid error-budget exhaustion. Page immediately.
- `SEV-2`: partial degradation with elevated risk. Engage on-call + service owner.
- `SEV-3`: contained issue, no immediate customer impact. Track in backlog with owner/date.

## Bridge roles

- `Incident Commander (IC)`: owns decisions, scope, and status cadence.
- `Ops Lead`: executes mitigations and validates recovery.
- `Comms Lead`: posts updates to stakeholders every 15 minutes for `SEV-1`, every 30 minutes for `SEV-2`.

## Initial response checklist (first 5 minutes)

1) Declare severity and assign roles.
2) Freeze risky changes (`kubectl -n sre-mini rollout pause deploy/sre-mini`) if needed.
3) Capture initial state:
- `kubectl -n sre-mini get pods -o wide`
- `kubectl -n sre-mini get events --sort-by=.metadata.creationTimestamp | tail -n 30`
- `kubectl -n sre-mini describe deploy sre-mini`

## Alert playbooks

### SREMiniErrorBudgetBurnFast (`SEV-1`)

1) Triage
- `kubectl -n sre-mini logs -l app=sre-mini --since=10m --tail=300`
- Check deployment changes in last hour:
  - `kubectl -n sre-mini rollout history deploy/sre-mini`

2) Containment
- If deploy-correlated: `kubectl -n sre-mini rollout undo deploy/sre-mini`
- If isolated bad pods: `kubectl -n sre-mini delete pod <pod-name>`

3) Recovery verification
- `kubectl -n sre-mini rollout status deploy/sre-mini --timeout=180s`
- Confirm burn-rate alert clears and 5xx ratio trends down in dashboards.

### SREMiniErrorBudgetBurnSlow (`SEV-2`)

1) Triage
- `kubectl -n sre-mini top pods`
- `kubectl -n sre-mini describe hpa sre-mini-hpa`
- Review latency/error trends over 6h.

2) Mitigation
- Scale safely if near resource ceilings:
  - `kubectl -n sre-mini scale deploy sre-mini --replicas=6`
- If recent change increased errors, rollback and re-baseline.

3) Follow-up
- Create action items for threshold tuning, dependency bottlenecks, or release quality gates.

### SREMiniHighLatency (`SEV-2`)

1) Triage
- `kubectl -n sre-mini top pods`
- `kubectl -n sre-mini describe hpa sre-mini-hpa`
- `kubectl -n sre-mini logs -l app=sre-mini --since=10m --tail=200`

2) Mitigation
- If underprovisioned and below HPA max:
  - `kubectl -n sre-mini scale deploy sre-mini --replicas=6`
- If rollout-related:
  - `kubectl -n sre-mini rollout undo deploy/sre-mini`

3) Recovery verification
- Confirm p95 latency drops below SLO and saturation metrics normalize.

### SREMiniHPAAtMax (`SEV-2`)

1) Triage
- `kubectl -n sre-mini describe hpa sre-mini-hpa`
- `kubectl -n sre-mini top pods`
- `kubectl -n sre-mini get pods -l app=sre-mini -o wide`

2) Temporary mitigation
- Raise max replicas with approval:
  - `kubectl -n sre-mini patch hpa sre-mini-hpa --type=merge -p '{"spec":{"maxReplicas":15}}'`

3) Recovery verification
- Confirm current replicas move below max and request latency/error trends recover.

## Incident closure and postmortem

1) Confirm customer impact window and final severity.
2) Re-enable paused rollouts if previously paused.
3) Open a postmortem from `postmortems/template.md` within 24 hours for `SEV-1` and `SEV-2`.
4) Track corrective actions with owner + due date; review completion in weekly reliability review.
