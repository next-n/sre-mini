# sre-mini (High-Assurance SRE Reference)

This repository demonstrates workload-level reliability engineering for a regulated, high-assurance environment.  
It is intentionally small, but every control in `k8s.yaml` is mapped to a concrete operational risk reduction objective.

## Engineering objectives

- Objective: production-minded SRE decisions, not just a working app.
- Scope: stateless service reliability, rollout safety, autoscaling, observability, and incident response.
- Focus: strong baseline at workload layer, with platform compliance controls called out as next steps.

## Repo structure

- `main.go`: Go HTTP service with health/readiness probes, graceful shutdown, metrics, and failure toggles.
- `k8s.yaml`: namespace, service account, deployment, service, PDB, HPA, ingress, and ServiceMonitor.
- `alerts/alerts.yaml`: Prometheus alert rules.
- `runbooks/incident.md`: operator response playbooks.
- `docs/slo.md`: SLI/SLO definitions and error-budget policy.
- `postmortems/template.md`: blameless postmortem template.
- `.github/workflows/ci.yml`: CI gates for quality, security, and supply-chain checks.
- `Dockerfile`: multi-stage build to distroless non-root runtime image.

## Service behavior (what is being protected)

- `GET /healthz`: liveness endpoint.
- `GET /readyz`: readiness endpoint.
- `GET|POST /fail/ready`: flips readiness to false.
- `GET|POST /recover/ready`: restores readiness to true.
- `GET /work`: simulated workload (2s latency) for autoscaling/latency testing.
- `GET /metrics`: Prometheus metrics.
- `GET /panic`: intentional crash path to validate self-healing.

## `k8s.yaml` deep dive with reliability rationale

### 1) Namespace: `sre-mini`

- Control: isolate workload resources from other tenants.
- Reliability rationale: reduces blast radius and enables cleaner RBAC, policy, and audit boundaries.

### 2) ServiceAccount: `sre-mini`

- Control: dedicated workload identity with token automount disabled.
- Reliability rationale: reduces credential exposure and enforces least-privilege by default.

### 3) Deployment: `sre-mini` (`apps/v1`)

- Control: `replicas: 3`.
- Reliability rationale: baseline high availability for single-pod failure tolerance.

- Control: rolling updates with `maxUnavailable: 1`, `maxSurge: 1`.
- Reliability rationale: limits customer impact during releases and supports progressive delivery safety.

- Control: `minReadySeconds`, `progressDeadlineSeconds`, and revision history retention.
- Reliability rationale: improves rollout safety, recovery options, and stuck-deploy detection.

- Control: readiness probe (`/readyz`) and liveness probe (`/healthz`).
- Reliability rationale: separates "can receive traffic" from "process is alive", reducing false recovery and bad routing.

- Control: startup probe on `/healthz`.
- Reliability rationale: prevents premature liveness restarts during cold start.

- Control: graceful shutdown path (`terminationGracePeriodSeconds: 15`, `preStop` readiness-fail hook, app readiness drop on SIGTERM).
- Reliability rationale: protects in-flight transactions during deploys, scale-down, and node events.

- Control: preferred pod anti-affinity on hostname.
- Reliability rationale: lowers correlated failure risk from single-node disruption.

- Control: resource requests/limits (`100m/128Mi` request, `500m/256Mi` limit).
- Reliability rationale: predictable scheduling and reduced noisy-neighbor instability.

- Control: hardened runtime (`runAsNonRoot`, `readOnlyRootFilesystem`, `allowPrivilegeEscalation: false`, drop all capabilities, seccomp runtime default).
- Reliability rationale: reduces container breakout and privilege escalation risk.

- Control: writable temp via `emptyDir` mounted at `/tmp`.
- Reliability rationale: maintains immutable root filesystem while supporting runtime temp needs.

### 4) Service: `sre-mini` (`v1`)

- Control: stable virtual IP fronting pods by label selector.
- Reliability rationale: decouples client routing from pod lifecycle for safer scaling and rollouts.

- Control: exposes `port 80 -> targetPort 8080`.
- Reliability rationale: consistent internal service contract for ingress and monitoring integrations.

### 5) PodDisruptionBudget: `sre-mini-pdb` (`policy/v1`)

- Control: `minAvailable: 2`.
- Reliability rationale: prevents voluntary operations (drain/upgrades) from dropping below minimum service capacity.

### 6) HorizontalPodAutoscaler: `sre-mini-hpa` (`autoscaling/v2`)

- Control: scale bounds `minReplicas: 3`, `maxReplicas: 10`.
- Reliability rationale: ensures minimum resilience floor and maximum cost/control ceiling.

- Control: dual native scaling signals (CPU 70% + memory 75%).
- Reliability rationale: balances compute and memory pressure without external metric dependencies.

- Control: tuned behavior windows and policies.
  - Scale up: fast reaction with capped burst (`100%` or `+4` per 15s, `selectPolicy: Max`, 60s stabilization).
  - Scale down: conservative contraction (`10%` or `-1` per 60s, `selectPolicy: Min`, 300s stabilization).
- Reliability rationale: avoids oscillation and protects customer experience under volatile demand.

### 7) Ingress: `sre-mini` (`networking.k8s.io/v1`)

- Control: host rule `sre-mini.local` on Traefik ingress class with TLS routing.
- Reliability rationale: clear north-south entry point and policy attachment location (TLS/WAF/rate limits at edge).

### 8) ServiceMonitor: `sre-mini` (`monitoring.coreos.com/v1`)

- Control: Prometheus Operator scrape config from `monitoring` namespace, selecting service label `app: sre-mini`.
- Reliability rationale: standardized telemetry ingestion for alerting, SLO tracking, and forensic triage.

## Reliability model

- Availability posture: 3 replicas + PDB + rollout constraints + readiness gating.
- Failure handling: crash recovery via liveness, traffic shedding via readiness, graceful termination on shutdown.
- Capacity posture: HPA on CPU and memory with dampened scale behavior to balance responsiveness and stability.
- Operability: structured request logs, Prometheus metrics, and runbooks for major incident classes.

## Alerting and incident response

- Alerts defined for:
  - fast error-budget burn (multi-window),
  - sustained error-budget burn,
  - high latency (p95),
  - HPA saturation at max replicas.
- Runbooks in `runbooks/incident.md` provide first-response commands and mitigation steps.
- Post-incident writeups are standardized with `postmortems/template.md`.

## SLO and error-budget policy

- SLOs and policy gates are documented in `docs/slo.md`.
- Availability target is defined with burn-rate driven alerts to balance release velocity and reliability risk.

## CI quality and security gates

- CI is defined in `.github/workflows/ci.yml`.
- Pipeline enforces formatting, `go vet`, race-enabled tests, build verification, `gosec`, and `govulncheck`.
- Supply-chain controls include SBOM generation plus filesystem/image vulnerability scans (Trivy).

## Production-readiness gaps for regulated, high-assurance environments (explicit)

This repository covers workload-level controls. Before production, add:

- Zero-trust networking: mTLS (service mesh) and default-deny `NetworkPolicy`.
- Identity and access: least-privilege RBAC bindings and policy-as-code gates.
- Secret management: external secret store integration (Vault/KMS) with rotation controls.
- Supply chain hardening: signed images, provenance enforcement, and admission-time policy checks.
- Governance and audit: immutable central logs, retention controls, and deployment approval trails.
- Resilience at platform layer: multi-AZ node groups, zone-aware scheduling, backup/restore, and DR exercises.

## Deploy and verify

```bash
# Provide TLS cert/key (or use cert-manager) before enabling ingress traffic
# kubectl -n sre-mini create secret tls sre-mini-tls --cert=tls.crt --key=tls.key

kubectl apply -f k8s.yaml
kubectl apply -f alerts/alerts.yaml

kubectl -n sre-mini get sa,deploy,po,svc,hpa,pdb,ing
kubectl -n monitoring get servicemonitor,prometheusrule | grep sre-mini
```

## Operational drills

```bash
# Readiness failure drill
kubectl -n sre-mini port-forward deploy/sre-mini 8080:8080
curl -XPOST http://localhost:8080/fail/ready
curl http://localhost:8080/readyz

# Recovery drill
curl -XPOST http://localhost:8080/recover/ready

# Load drill
for i in $(seq 1 20); do curl -s http://localhost:8080/work > /dev/null; done
```
