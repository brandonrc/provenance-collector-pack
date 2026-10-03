# Build stage. The base image only bootstraps Go: the exact toolchain is the
# `toolchain` line in go.mod (the one place it is pinned), which CI and the
# release binaries use too.
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src

# The golang image sets GOTOOLCHAIN=local; clear it so `go env -w` applies.
ENV GOTOOLCHAIN=
COPY go.mod go.sum ./
RUN go env -w GOTOOLCHAIN="$(awk '/^toolchain /{print $2}' go.mod)" && go version && go mod download

COPY . .

ARG VERSION=dev
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build \
    -ldflags="-s -w -X github.com/nebari-dev/provenance-collector/internal/report.Version=${VERSION}" \
    -o /provenance-collector \
    ./cmd/provenance-collector

RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build \
    -ldflags="-s -w" \
    -o /dashboard \
    ./cmd/dashboard

# Runtime stage — distroless for minimal attack surface
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /provenance-collector /provenance-collector
COPY --from=builder /dashboard /dashboard

USER nonroot:nonroot

ENTRYPOINT ["/provenance-collector"]
