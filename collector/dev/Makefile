# Local development helpers for the Go collector (run from collector/dev/).
#
# The pack's chart (../../chart) no longer deploys the collector CronJob or the
# Go dashboard: the security posture worker runs the collector binary once per
# scan. These targets exercise the binary and the standalone dashboard locally
# against whatever cluster your KUBECONFIG points at (kind, grace, ...).

DASHBOARD_ADDR          ?= :8080
DASHBOARD_INTERNAL_ADDR ?= :8081
REPORTS                 ?= $(CURDIR)/.reports

.PHONY: build once dashboard seed clean

build:
	$(MAKE) -C .. build

# One collection, report JSON on stdout (logs on stderr), exactly what the
# worker runs. Signature/update checks are off by default here to keep it fast;
# override with PROVENANCE_VERIFY_SIGNATURES=true etc.
once: build
	PROVENANCE_VERIFY_SIGNATURES=$${PROVENANCE_VERIFY_SIGNATURES:-false} \
	PROVENANCE_CHECK_UPDATES=$${PROVENANCE_CHECK_UPDATES:-false} \
		../bin/provenance-collector --once --output - | jq .summary

# Standalone dashboard (API only) on :8080, internal upload endpoint on :8081,
# reports under ./.reports. Auth is disabled when PROVENANCE_OIDC_ISSUER is unset.
dashboard: build
	mkdir -p $(REPORTS)
	PROVENANCE_DASHBOARD_ADDR=$(DASHBOARD_ADDR) \
	PROVENANCE_DASHBOARD_INTERNAL_ADDR=$(DASHBOARD_INTERNAL_ADDR) \
	PROVENANCE_REPORT_PATH=$(REPORTS) \
		../bin/dashboard

# Seed the running standalone dashboard with sample reports (timeline history).
seed:
	python3 seed-reports.py http://localhost:8081/internal/reports

clean:
	rm -rf $(REPORTS)
