# Go collector (provenance-collector) and standalone dashboard. Run from collector/.
VERSION ?= dev
IMAGE   ?= ghcr.io/nebari-dev/provenance-collector
TAG     ?= $(VERSION)

LDFLAGS := -s -w -X github.com/nebari-dev/provenance-collector/internal/report.Version=$(VERSION)

.PHONY: build test lint docs docs-check checkenvs docker-build docker-push helm-lint run-once clean

build:
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o bin/provenance-collector ./cmd/provenance-collector
	CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/dashboard ./cmd/dashboard

test:
	go test -race -coverprofile=coverage.out ./...

lint:
	golangci-lint run ./...

# Regenerate ../docs/src/content/docs/collector-environment.md from
# internal/configspec/spec.go.
docs:
	go run ./hack/gendocs

# CI guard: fail if collector-environment.md is stale relative to the spec OR
# if a source file references an env var not present in the spec.
docs-check: checkenvs
	go run ./hack/gendocs --check

# Static check that every os.Getenv("...") in cmd/ and internal/ refers to a
# name listed in internal/configspec/spec.go.
checkenvs:
	go run ./hack/checkenvs

docker-build:
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE):$(TAG) .

docker-push: docker-build
	docker push $(IMAGE):$(TAG)

# The chart lives at the repository root (../chart) and deploys the security
# posture api/worker/ui; the worker image bundles this collector.
helm-lint:
	helm dependency build ../chart
	helm lint ../chart
	helm template test ../chart | kubeconform -strict -ignore-missing-schemas

# Single run against the current kubeconfig, report to stdout (logs on stderr).
run-once: build
	./bin/provenance-collector --once --output - > provenance-report.json

clean:
	rm -rf bin/ coverage.out provenance-report.json
