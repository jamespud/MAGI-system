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

# ca-certificates: the server makes outbound HTTPS calls to the model API/Tavily.
RUN apk add --no-cache ca-certificates

COPY --from=builder /app/magi-server /app/magi-server
# Bake only the structural config (placeholder secrets). Real secrets/DSN are
# injected at runtime via env vars (see bootstrap.applyEnvOverrides).
COPY --from=builder /app/conf/magi.yaml.example /app/conf/magi.yaml

WORKDIR /app
EXPOSE 8080

CMD ["/app/magi-server"]
