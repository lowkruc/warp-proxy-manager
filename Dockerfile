FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /warp-proxy-manager ./cmd/manager/
RUN CGO_ENABLED=0 GOOS=linux go build -o /warpctl ./cmd/cli/

FROM alpine:3.19

RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=builder /warp-proxy-manager .
COPY --from=builder /warpctl /usr/local/bin/
COPY config.example.yaml .

RUN mkdir -p /app/data

EXPOSE 1080 8080

ENTRYPOINT ["/app/warp-proxy-manager"]
CMD ["-config", "/app/config.yaml"]
