FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY backend/ ./

RUN CGO_ENABLED=0 go build -mod=vendor -ldflags="-s -w" -o magi-server ./cmd/magi-server

FROM alpine:3.21

COPY --from=builder /app/magi-server /app/magi-server
COPY --from=builder /app/conf /app/conf

WORKDIR /app
EXPOSE 8080

CMD ["/app/magi-server"]
