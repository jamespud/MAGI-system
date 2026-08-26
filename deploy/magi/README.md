# MAGI Helm Chart

This chart deploys the stateless MAGI backend and nginx frontend. It assumes MySQL, Milvus, and Elasticsearch are reachable from the cluster and configured through Kubernetes DNS in the database/RAG endpoints.

## Build and publish images

The backend and frontend images are built from their own contexts:

```bash
export REGISTRY=ghcr.io/your-org
export VERSION=0.1.0

docker build -t "$REGISTRY/magi-server:$VERSION" backend
docker build -t "$REGISTRY/magi-frontend:$VERSION" frontend
docker push "$REGISTRY/magi-server:$VERSION"
docker push "$REGISTRY/magi-frontend:$VERSION"
```

## Create a production values file

Do not put real keys in Git. Create a local `magi-values.yaml`:

```yaml
backend:
  image:
    repository: ghcr.io/your-org/magi-server
    tag: 0.1.0
  replicaCount: 1

frontend:
  image:
    repository: ghcr.io/your-org/magi-frontend
    tag: 0.1.0

configuration:
  modelBaseURL: https://api.deepseek.com
  modelName: your-model
  milvusAddress: your-milvus:19530
  esAddresses: http://your-elasticsearch:9200

secret:
  create: true
  values:
    dbDSN: "magi:password@tcp(your-mysql:3306)/magi?charset=utf8mb4&parseTime=True"
    modelApiKey: "sk-your-model-key"
    tavilyApiKey: "tvly-your-tavily-key"
    embeddingApiKey: ""
    # REQUIRED: replace this non-parseable placeholder before deployment with
    # userID:role:name:<newly-generated-random-secret>; never use this value.
    authAPIKeys: "REPLACE_WITH_GENERATED_BOOTSTRAP_SPEC"

ingress:
  enabled: true
  className: nginx
  hosts:
    - host: magi.example.com
      paths:
        - path: /
          pathType: Prefix
```

For an externally managed secret, set:

```yaml
secret:
  create: false
  existingSecret: magi-secrets
```

The external secret must contain these keys (or the matching configured `secret.keys` values):

- `db-dsn`
- `model-api-key`
- `tavily-api-key` (may be empty)
- `embedding-api-key` (may be empty)
- `auth-api-keys` (must be non-empty when A2A is enabled; DB-issued keys may be used after bootstrap)

Keep `configuration.a2a.enabled` set to `false` unless the deployment has a
stable HTTPS public URL and authentication enabled. Global API authentication
is enabled by default; keep `configuration.authEnabled: "true"` unless an
explicitly unauthenticated deployment is intended. When A2A is enabled,
`configuration.authEnabled` must be `"true"`, and `auth-api-keys` must contain
at least one bootstrap API-key specification. This requirement also applies to
an externally managed Secret: the chart cannot inspect its contents, so the
cluster operator must ensure the value is present before enabling A2A.

## Install and upgrade

```bash
kubectl create namespace magi

helm upgrade --install magi deploy/magi \
  --namespace magi \
  --create-namespace \
  --values magi-values.yaml
```

Watch rollout:

```bash
kubectl -n magi rollout status deploy/magi-backend
kubectl -n magi rollout status deploy/magi-frontend
```

The backend readiness probe uses `/ready`; liveness uses `/health`. The frontend nginx proxies `/api/` and `/health` to the backend and preserves long-lived SSE responses.

## External dependencies

This chart intentionally does not embed MySQL, Milvus, Elasticsearch, etcd, or MinIO. Provision them with the operator/chart supported by your platform, create persistent backups, and point `secret.values.dbDSN`, `configuration.milvusAddress`, and `configuration.esAddresses` at those services.

## Horizontal scaling and availability

1. Keep `backend.replicaCount=1` for the first install while the application initializes its schema.
2. After a successful rollout, scale the backend:

```bash
kubectl -n magi scale deploy/magi-backend --replicas=2
```

or enable the backend HPA:

```yaml
backend:
  autoscaling:
    enabled: true
    minReplicas: 2
    maxReplicas: 5
    targetCPUUtilizationPercentage: 75
  pdb:
    enabled: true
    minAvailable: 1
```

Multiple backend replicas are supported by shared database state, durable jobs, DB-backed scheduler locks, shared user quotas, and SSE DB polling. Ensure MySQL and RAG services are sized for the additional load. The frontend is stateless and defaults to two replicas, a PDB, and optional CPU autoscaling.

Ingress annotations disable proxy buffering and extend read/send timeouts for SSE streaming. For TLS, configure `ingress.tls` and certificates with your ingress controller.

## A2A rollout

A2A is disabled by default and its database schema is gated by migration
scripts that must be applied in order. The following phases assume you are
upgrading an existing deployment. Never enable A2A before every phase below
completes, and never roll back the binary to a pre-S16 writer once S16 has run.

### Migration order

1. **Drain pre-S16 event writers.** Stop the old binary or scale its Deployment
   to zero so no process still writes `event_sequence` rows under the legacy
   contract. Back up MySQL (mysqldump/xtrabackup) and record the backup name.
2. **Apply `magi_s16_event_sequence.sql`.** Run the S16 schema change, then
   validate that no Case with events has a NULL, duplicate, missing, or stale
   event cursor. The backend refuses to start (S16 startup check) when such a
   row exists, so fix forward: repair cursors or re-insert the missing durable
   event before proceeding. Do not roll back to a pre-S16 writer after this
   step; a downgrade is only a coordinated restore of the pre-S16 backup.
3. **Apply `magi_s17_a2a_submission.sql`**, then the additive
   `magi_s18_a2a_start_claim.sql` **before** the A2A-capable binary starts.
   Both scripts are additive and safe to run while the old binary is still
   running, but S18 must be present before A2A admission begins.

### Enablement sequence

4. **Deploy with A2A disabled.** Set `configuration.a2a.enabled=false` and
   verify the rollout is healthy (`/ready`) before changing anything else.
5. **Configure prerequisites.** Set `configuration.authEnabled=true`, provide a
   non-empty chart-created or external `auth-api-keys` Secret, configure an
   HTTPS `configuration.a2a.publicURL` (origin only, no path/query/fragment),
   terminate TLS at the ingress with `ingress.tls`, and keep the nginx
   streaming annotations (proxy buffering off, long read/send timeouts) in
   place. Rendering with A2A enabled but auth disabled, or with an empty
   chart-created `auth-api-keys`, fails by design.
6. **Enable one canary replica.** Set `configuration.a2a.enabled=true` with
   `backend.replicaCount=1`. Fetch the public Agent Card and verify its
   `url` is exactly `https://<host>/a2a` and the security schemes are bearer
   and `X-API-Key`.
7. **Smoke test.** Send one task with `message:send`, subscribe to its stream,
   and verify the event sequence is ordered, exactly one terminal state is
   emitted, and the audit trail (`a2a.send`) plus `magi_a2a_*` metrics record
   the run. Confirm the optional `/metrics` scrape parses cleanly.
8. **Widen the rollout.** Scale the backend to the desired replica count. The
   stream limit is **per replica** (`configuration.a2a.maxStreamsPerUserPerReplica`),
   so the effective per-user ceiling is `replicas x per-replica limit`. A
   stricter global connection budget must be enforced at the ingress; this
   chart does not add a distributed semaphore.

### Failure recovery

- If the S16 cursor validation fails, fix the data forward (repair cursors or
  missing events) and restart. Do not continue with a partially migrated
  writer.
- If A2A misbehaves after rollout, disable it by setting
  `configuration.a2a.enabled=false` and re-running `helm upgrade`; keep the
  A2A-capable binary in place. Do not downgrade the binary while S16/S17/S18
  are applied.
- A full rollback means restoring the pre-S16 backup and then redeploying the
  matching older binary, as one coordinated operation, not an in-place
  downgrade.
