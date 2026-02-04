# sre-mini test cheatsheet (Linux/Ubuntu)

Use this file to validate all major features quickly on Linux/Ubuntu.

> Run all drills in a non-production environment.

## 0) Prerequisites

- `kubectl` connected to a cluster
- Prometheus Operator stack installed (`ServiceMonitor`, `PrometheusRule`)
- Traefik ingress class available
- `docker` installed locally
- `curl` installed

## 1) Local app smoke test (Docker)

```bash
docker build -t sre-mini:test .

cid=$(docker run -d -p 18080:8080 sre-mini:test)
curl -fsS http://localhost:18080/healthz
curl -fsS http://localhost:18080/readyz
curl -fsS http://localhost:18080/work
curl -fsS http://localhost:18080/metrics | head -n 20

# readiness toggle
curl -fsS -XPOST http://localhost:18080/fail/ready
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:18080/readyz
curl -fsS -XPOST http://localhost:18080/recover/ready
curl -fsS http://localhost:18080/readyz

docker rm -f "$cid"
```

## 2) Deploy to Kubernetes

```bash
# optional: create TLS secret used by ingress
# kubectl -n sre-mini create secret tls sre-mini-tls --cert=tls.crt --key=tls.key

kubectl apply -f k8s.yaml
kubectl apply -f alerts/alerts.yaml

kubectl -n sre-mini get sa,deploy,po,svc,hpa,pdb,ing
kubectl -n monitoring get servicemonitor,prometheusrule | grep sre-mini
kubectl -n sre-mini rollout status deploy/sre-mini --timeout=180s
```

## 3) Core endpoint drills (in-cluster)

Terminal 1:

```bash
kubectl -n sre-mini port-forward svc/sre-mini 8080:80
```

Terminal 2:

```bash
curl -fsS http://localhost:8080/healthz
curl -fsS http://localhost:8080/readyz
curl -fsS http://localhost:8080/work
curl -fsS http://localhost:8080/metrics | head -n 20

# fail + recover readiness
curl -fsS -XPOST http://localhost:8080/fail/ready
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8080/readyz
curl -fsS -XPOST http://localhost:8080/recover/ready
curl -fsS http://localhost:8080/readyz
```

## 4) Deployment reliability checks

```bash
# probes and lifecycle
pod=$(kubectl -n sre-mini get pod -l app=sre-mini -o jsonpath='{.items[0].metadata.name}')
kubectl -n sre-mini describe pod "$pod" | grep -E "Liveness|Readiness|Startup|PreStop"

# anti-affinity spread (best seen with 2+ nodes)
kubectl -n sre-mini get pods -o wide

# checking security posture

#The app can still run normally. It just cannot control or query the cluster
kubectl -n sre-mini get deploy sre-mini -o jsonpath='{.spec.template.spec.automountServiceAccountToken}{"\n"}'
#seccomp does NOT block kernel access — it blocks dangerous kernel access.
kubectl -n sre-mini get deploy sre-mini -o jsonpath='{.spec.template.spec.securityContext.seccompProfile.type}{"\n"}'
#Makes the container’s root filesystem read-only. Read-only root = “you can run, but you can’t modify yourself.”
kubectl -n sre-mini get deploy sre-mini -o jsonpath='{.spec.template.spec.containers[0].securityContext.readOnlyRootFilesystem}{"\n"}'
#Prevents the process from gaining more privileges than it started with. false ✅→ App cannot become root or gain extra power
kubectl -n sre-mini get deploy sre-mini -o jsonpath='{.spec.template.spec.containers[0].securityContext.allowPrivilegeEscalation}{"\n"}'
#Removes all special Linux “superpowers” from the process.
kubectl -n sre-mini get deploy sre-mini -o jsonpath='{.spec.template.spec.containers[0].securityContext.capabilities.drop}{"\n"}'
```

## 5) Self-heal and rollout drills

Terminal 1:

```bash
kubectl -n sre-mini port-forward svc/sre-mini 8080:80
```

Terminal 2:

```bash
curl -s http://localhost:8080/panic >/dev/null || true
kubectl -n sre-mini get pods -w
```

Rollout failure + rollback:

```bash
kubectl -n sre-mini set image deploy/sre-mini app=docker.io/nextn1/sre-mini:does-not-exist
kubectl -n sre-mini rollout status deploy/sre-mini --timeout=240s

kubectl -n sre-mini rollout pause deploy/sre-mini
kubectl -n sre-mini rollout undo deploy/sre-mini
kubectl -n sre-mini rollout status deploy/sre-mini --timeout=180s
```

## 6) PDB, HPA, ingress, monitoring checks

```bash
kubectl -n sre-mini describe pdb sre-mini-pdb
#“Events explain the past, Conditions describe the present.”
kubectl -n sre-mini describe hpa sre-mini-hpa
kubectl -n sre-mini get ingress sre-mini -o yaml | grep -E "ingressClassName|tls|secretName|websecure"
kubectl -n monitoring get servicemonitor sre-mini -o yaml
kubectl -n monitoring get prometheusrule sre-mini-alerts -o yaml
```

## 7) HPA autoscale drill

```bash
# lower CPU threshold temporarily
kubectl -n sre-mini patch hpa sre-mini-hpa --type='json' -p='[{"op":"replace","path":"/spec/metrics/0/resource/target/averageUtilization","value":1}]'
```

Terminal 1:

```bash
kubectl -n sre-mini port-forward svc/sre-mini 8080:80
```

Terminal 2:

```bash
seq 800 | xargs -n1 -P50 sh -c 'curl -s http://localhost:8080/work >/dev/null'
kubectl -n sre-mini get hpa -w
kubectl -n sre-mini get pods -w

```

Restore HPA target:

```bash
kubectl -n sre-mini patch hpa sre-mini-hpa --type='json' -p='[{"op":"replace","path":"/spec/metrics/0/resource/target/averageUtilization","value":70}]'
```

## 8) Alert drills (burn-rate, latency, saturation)

```bash
kubectl -n monitoring get svc | grep prometheus
# example:
# kubectl -n monitoring port-forward svc/kps-kube-prometheus-stack-prometheus 9090:9090
```
```bash
kubectl -n monitoring port-forward svc/kps-kube-prometheus-stack-prometheus 9090:9090
#Drill for test
kubectl apply -f alerts/alerts-drill.yaml
```
Prometheus UI: `http://localhost:9090`

- `SREMiniErrorBudgetBurnFast`: high `/panic` rate for several minutes(5s for drill)
- `SREMiniErrorBudgetBurnSlow`: moderate `/panic` rate over longer window(10s for drill)
- `SREMiniHighLatency`: keep `/work` traffic steady for 5+ minutes()(10sec for drill)
- `SREMiniHPAAtMax`: cap max replicas to current and wait 10+ minutes

```bash
kubectl -n sre-mini patch hpa sre-mini-hpa --type merge -p '{"spec":{"maxReplicas":3}}'
kubectl -n sre-mini describe hpa sre-mini-hpa

# restore
kubectl -n sre-mini patch hpa sre-mini-hpa --type merge -p '{"spec":{"maxReplicas":10}}'
```

## 9) Incident workflow and postmortem

```bash
cat runbooks/incident.md
cp postmortems/template.md postmortems/INC-YYYYMMDD-summary.md
```

## 10) CI checks locally

```bash
go test ./...
docker build -t sre-mini:test .

# optional: lint workflow
docker run --rm -v "$(pwd):/repo" -w /repo rhysd/actionlint:latest .github/workflows/ci.yml
```

## 11) Cleanup

```bash
kubectl delete -f alerts/alerts.yaml
kubectl delete -f k8s.yaml
```

Your snippet **doesn’t include the k3d cluster name** (that output is just the HPA watch). So in Step 12 I’ll make it **bulletproof**: first we **discover the cluster name**, then we add a node, then we raise `maxReplicas`, then we follow a banking-style protocol.

Paste this as **Step 12** in `cheatsheet.md`:

---

## 12) SEV-1 protocol drill — Capacity exhaustion (HPA at max)

This simulates a **production-grade capacity incident**: autoscaling exists, but the service is **pinned at max replicas** and needs **more capacity**.

**Production note:** Node scaling is typically **automatic**, but bounded by **approved limits** (max nodes/budget/quotas). When limits are hit, raising them is a **human decision** (SEV-1).

### 12.0 Enable drill rules and open Prometheus UI

```bash
kubectl apply -f alerts/alerts-drill.yaml
kubectl -n monitoring port-forward svc/kps-kube-prometheus-stack-prometheus 9090:9090
```

Prometheus UI: `http://localhost:9090`

---

### 12.1 Trigger SREMiniHPAAtMax

**Cap HPA so it can hit max quickly:**

```bash
kubectl -n sre-mini patch hpa sre-mini-hpa --type merge -p '{"spec":{"maxReplicas":6}}'
kubectl -n sre-mini describe hpa sre-mini-hpa
```

**Generate load:**

Terminal 1:

```bash
kubectl -n sre-mini port-forward svc/sre-mini 8080:80
```

Terminal 2:

```bash
seq 5000 | xargs -n1 -P80 sh -c 'curl -s http://localhost:8080/work/cpu >/dev/null'
kubectl -n sre-mini get hpa -w
```

**Confirm:**

* `SREMiniHPAAtMax` is **FIRING**
* `kubectl -n sre-mini get hpa -w` shows **REPLICAS == MAXPODS**

---

### 12.2 Declare SEV-1 and freeze risk

```bash
kubectl -n sre-mini rollout pause deploy/sre-mini
```

Roles (banking standard):

* IC (Incident Commander)
* Ops (K8s/infra)
* Comms (stakeholder updates)

---

### 12.3 Evidence collection (no guessing)

```bash
kubectl -n sre-mini describe hpa sre-mini-hpa
kubectl -n sre-mini get pods -o wide
kubectl get nodes -o wide
kubectl -n sre-mini get events --sort-by=.metadata.creationTimestamp | tail -n 40
kubectl -n sre-mini logs -l app=sre-mini --since=10m --tail=300
```

What you’re proving:

* App pods are healthy (not crashloop)
* HPA is functioning (but pinned)
* This is **capacity exhaustion**, not a code bug

---

### 12.4 Containment — expand capacity (drill uses k3d “add node”)

#### A) Add a node (simulate bank “node autoscale happened / approved”)

**Find your k3d cluster name:**

```bash
k3d cluster list
```

**Add a new agent node (replace `<CLUSTER>`):**

```bash
k3d node create sre-agent-2 --cluster <CLUSTER> --role agent
```

Verify node joined:

```bash
kubectl get nodes
```

> If pods don’t spread, you may need to scale replicas above 1 node’s capacity (next step) so scheduler places pods on the new node.

#### B) Raise HPA maxReplicas (simulate “approved limit increased”)

```bash
kubectl -n sre-mini patch hpa sre-mini-hpa --type merge -p '{"spec":{"maxReplicas":8}}'
kubectl -n sre-mini get hpa -w
kubectl -n sre-mini get pods -w
```

Containment success criteria:

* HPA no longer pinned at max
* Pods spread across nodes
* Latency stabilizes, errors do not spike

---

### 12.5 Recovery validation and stabilize

```bash
curl -fsS http://localhost:8080/readyz
curl -fsS http://localhost:8080/work >/dev/null
kubectl -n sre-mini describe hpa sre-mini-hpa
kubectl -n sre-mini get pods -o wide
kubectl get nodes -o wide
```

Confirm:

* `SREMiniHPAAtMax` is **RESOLVED**
* Headroom exists again (replicas < maxReplicas)

---

### 12.6 Stand down + postmortem (banking expectations)

```bash
kubectl -n sre-mini rollout resume deploy/sre-mini
cp postmortems/template.md postmortems/INC-YYYYMMDD-capacity-exhaustion.md
```

Postmortem topics (capacity incident):

* Why was max capacity insufficient?
* What was the approved ceiling and why?
* Forecasting / load testing gaps?
* New approved limits + alerting thresholds
* Time-to-scale and time-to-approval

---

### 12.7 Restore normal configuration

```bash
kubectl -n sre-mini patch hpa sre-mini-hpa --type merge -p '{"spec":{"maxReplicas":10}}'
kubectl delete -f alerts/alerts-drill.yaml
kubectl apply -f alerts/alerts.yaml
```
