# NOTE: this root-level Dockerfile is superseded by backend/Dockerfile.
# All build paths (docker-compose-web.yml, scripts/build.sh) use the backend
# context; the root file is kept only as a historical reference and may drift
# from go.mod. Delete it once nothing references it.
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Build-arg so China builds can swap to a faster mirror (e.g. aliyun).
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY}

COPY . ./

# -mod=mod fetches the published coze-studio module from the proxy; the build no
# longer depends on a local vendor dir or a local coze-studio checkout.
RUN CGO_ENABLED=0 go build -mod=mod -ldflags="-s -w" -o magi-server ./cmd/magi-server

FROM alpine:3.21

# ca-certificates: outbound HTTPS (model API/Tavily). python3+deno run the
# vendored Coze sandbox orchestrator (backend/sandbox.py -> pyodide-sandbox).
RUN apk add --no-cache ca-certificates python3 deno

COPY --from=builder /app/magi-server /app/magi-server
# Vendored Coze sandbox orchestrator (see backend/sandbox.py header for provenance).
COPY --from=builder /app/backend/sandbox.py /app/sandbox.py
# Bake only the structural config (placeholder secrets). Real secrets/DSN are
# injected at runtime via env vars (see bootstrap.applyEnvOverrides).
COPY --from=builder /app/conf/magi.yaml.example /app/conf/magi.yaml

WORKDIR /app
# Warm the pyodide-sandbox module cache so sandbox executions work offline.
RUN deno run -A jsr:@langchain/pyodide-sandbox@0.0.4 -c "print('ok')"
EXPOSE 8080

CMD ["/app/magi-server"]
