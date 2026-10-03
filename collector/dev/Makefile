# Local development helpers using kind

CLUSTER_NAME ?= provenance-dev
RELEASE      ?= pc
NAMESPACE    ?= provenance-system

.PHONY: cluster-create cluster-delete build-load deploy test-run logs report down \
        ui-up seed ui-forward ui-dev

cluster-create:
	kind create cluster --name $(CLUSTER_NAME)

cluster-delete:
	kind delete cluster --name $(CLUSTER_NAME)

build-load:
	docker build --build-arg VERSION=dev -t provenance-collector:dev ..
	kind load docker-image provenance-collector:dev --name $(CLUSTER_NAME)

deploy: build-load
	helm upgrade --install pc ../chart/ \
		--set image.repository=provenance-collector \
		--set image.tag=dev \
		--set image.pullPolicy=Never \
		--set persistence.mode=configmap \
		--set webUI.enabled=false \
		--set config.verifySignatures=false \
		--set config.checkUpdates=false

test-run:
	kubectl delete job manual-run --ignore-not-found
	kubectl create job --from=cronjob/pc-provenance-collector manual-run
	kubectl wait --for=condition=complete job/manual-run --timeout=120s
	kubectl logs job/manual-run

logs:
	kubectl logs job/manual-run

report:
	kubectl get configmap provenance-report -o jsonpath='{.data.report\.json}' | jq .

down:
	kind delete cluster --name $(CLUSTER_NAME)

# -----------------------------------------------------------------------------
# UI development: deploy the chart with the web dashboard (API) enabled (http
# mode) and seed it with rich sample reports, then run the React UI via Vite
# (`make ui-dev`). The dashboard is now API-only — the SPA is served by Vite in
# dev (and by the standalone nginx `frontend` image in production). Use these
# for dashboard/UI work; the `deploy` target above runs the collector headless
# (configmap mode, no UI).
# -----------------------------------------------------------------------------

# Build + load the image, then install the chart with the dashboard API turned
# on. frontend.enabled=false because in dev the SPA is served by Vite, not the
# in-cluster nginx image (which isn't built locally) — see `ui-dev`.
ui-up: build-load
	helm upgrade --install $(RELEASE) ../chart/ \
		--namespace $(NAMESPACE) --create-namespace \
		--set image.repository=provenance-collector \
		--set image.tag=dev \
		--set image.pullPolicy=Never \
		--set webUI.enabled=true \
		--set frontend.enabled=false \
		--set config.verifySignatures=false \
		--set webUI.features.timelineDeltas=true
	kubectl rollout status deploy/$(RELEASE)-provenance-collector-web \
		-n $(NAMESPACE) --timeout=120s

# Run the React dev server (Vite) against the in-cluster dashboard API. Opens a
# port-forward to the dashboard (localhost:8080), which Vite proxies /api to,
# and starts Vite at http://localhost:5173 with auth bypassed
# (VITE_DEV_NO_AUTH=true) to match the dashboard's disabled-auth dev posture.
ui-dev:
	@echo "Vite dev server -> http://localhost:5173  (proxying /api to the dashboard)"
	@kubectl port-forward -n $(NAMESPACE) \
		svc/$(RELEASE)-provenance-collector-web 8080:8080 >/dev/null 2>&1 & \
	pf=$$!; \
	trap "kill $$pf 2>/dev/null" EXIT; \
	for i in 1 2 3 4 5; do \
		curl -sf http://localhost:8080/healthz >/dev/null 2>&1 && break; sleep 1; \
	done; \
	cd ../frontend && VITE_DEV_NO_AUTH=true WEBAPI_URL=http://localhost:8080 npm run dev

# Seed the dashboard with three timestamped sample reports. Opens a temporary
# port-forward to the internal upload Service, uploads, then tears it down.
seed:
	@echo "seeding sample reports via internal upload endpoint..."
	@kubectl port-forward -n $(NAMESPACE) \
		svc/$(RELEASE)-provenance-collector-web-internal 8081:8081 >/dev/null 2>&1 & \
	pf=$$!; \
	trap "kill $$pf 2>/dev/null" EXIT; \
	for i in 1 2 3 4 5; do \
		curl -sf http://localhost:8081/healthz >/dev/null 2>&1 && break; sleep 1; \
	done; \
	python3 seed-reports.py http://localhost:8081/internal/reports

# Port-forward the public dashboard (blocks). Open http://localhost:8080.
ui-forward:
	@echo "dashboard at http://localhost:8080 (Ctrl+C to stop)"
	kubectl port-forward -n $(NAMESPACE) \
		svc/$(RELEASE)-provenance-collector-web 8080:8080
