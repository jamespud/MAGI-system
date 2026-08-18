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
    authAPIKeys: ""

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
- `auth-api-keys` (may be empty when DB-issued API keys are used)

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
