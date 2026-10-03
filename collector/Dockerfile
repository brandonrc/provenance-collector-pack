# Build stage
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -X github.com/nebari-dev/provenance-collector/internal/report.Version=${VERSION}" \
    -o /provenance-collector \
    ./cmd/provenance-collector

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w" \
    -o /dashboard \
    ./cmd/dashboard

# Runtime stage — distroless for minimal attack surface
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /provenance-collector /provenance-collector
COPY --from=builder /dashboard /dashboard

USER nonroot:nonroot

ENTRYPOINT ["/provenance-collector"]
