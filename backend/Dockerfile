FROM golang:1.25-alpine AS builder

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
# Build-arg so China builds can swap to a faster mirror (e.g. aliyun), same
# idea as the GOPROXY build-arg above.
ARG ALPINE_MIRROR=https://mirrors.aliyun.com/alpine
RUN printf '%s/v3.21/main\n%s/v3.21/community\n' "$ALPINE_MIRROR" "$ALPINE_MIRROR" > /etc/apk/repositories
RUN apk add --no-cache ca-certificates python3 deno

COPY --from=builder /app/magi-server /app/magi-server
# Vendored Coze sandbox orchestrator (see backend/sandbox.py header for provenance).
COPY --from=builder /app/sandbox.py /app/sandbox.py
# Bake only the structural config (placeholder secrets). Real secrets/DSN are
# injected at runtime via env vars (see bootstrap.applyEnvOverrides).
COPY --from=builder /app/conf/magi.yaml.example /app/conf/magi.yaml

WORKDIR /app
# Warm the pyodide-sandbox module cache so sandbox executions work offline.
RUN deno run -A jsr:@langchain/pyodide-sandbox@0.0.4 -c "print('ok')"
EXPOSE 8080

CMD ["/app/magi-server"]
