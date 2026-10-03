---
title: NebariApp CRD Reference
description: The NebariApp custom resource the chart renders, how chart values map onto it, and the operator's field reference.
---

With `nebariapp.enabled: true` the chart renders one `NebariApp`
(`reconcilers.nebari.dev/v1`) through the `nebari-app` library chart. The
nebari-operator turns it into an HTTPRoute on the shared gateway, a cert-manager
Certificate, a Keycloak client (Secret `<fullname>-oidc-client`) and, with gateway
enforcement, an Envoy `SecurityPolicy`. The landing page tile comes from
`landingPage`.

## What the chart renders

`helm template provenance-collector chart/ -n provenance-system -f examples/nebari-values.yaml`:

```yaml
apiVersion: reconcilers.nebari.dev/v1
kind: NebariApp
metadata:
  name: provenance-collector          # <fullname>
  namespace: provenance-system
spec:
  hostname: provenance.example.com    # nebariapp.hostname (required)
  service:
    name: provenance-collector-ui     # the ui Service (nginx)
    port: 80                          # ui.service.port
  routing:
    routes:
      - pathPrefix: /
        pathType: PathPrefix
    publicRoutes:                     # reachable without login
      - pathPrefix: /healthz          # landing-page health check
        pathType: Exact
      - pathPrefix: /icon.svg         # landing-page tile icon
        pathType: Exact
    tls:
      enabled: true
  auth:
    enabled: true
    provider: keycloak
    provisionClient: true
    enforceAtGateway: true            # false when adminGate.securityPolicy.enabled
    forwardAccessToken: true          # the api verifies the JWT itself
    redirectURI: /oauth2/callback
    scopes: [openid, profile, email, groups]
    groups: [admin]                   # = adminGroups
  gateway: public
  landingPage:
    enabled: true
    displayName: Security Posture
    description: Container vulnerability & configuration posture across the cluster
    icon: https://provenance.example.com/icon.svg
    category: Platform
    priority: 20
    healthCheck: { enabled: true, path: /healthz, intervalSeconds: 30, timeoutSeconds: 5 }
```

Every field comes from `nebariapp.*` in `values.yaml`; string values containing
`{{` are templated (for example `groups: '{{ .Values.adminGroups | toJson }}'`).

## The admin gate

The pack is admin-only, enforced in up to three layers:

1. **Gateway, via the NebariApp.** `auth.groups` = `adminGroups`. Operators that
   enforce groups at the gateway reject everybody else before the request reaches
   the pack.
2. **Gateway, via the chart's own SecurityPolicy** (`adminGate.securityPolicy.enabled`).
   For operator versions that use `auth.groups` only for landing-page visibility:
   the chart renders an Envoy `SecurityPolicy` (OIDC + JWT + authorization
   default deny, allow on the `groups` claim) on the operator's HTTPRoute and sets
   `enforceAtGateway: false` so the route carries exactly one policy.
3. **API.** The api verifies the token signature (JWKS), issuer (`auth.issuers`)
   and group membership (`adminGroups`) on every request; `GET /api/v1/me`
   answers non-admins so the UI can explain the 403.

## Operator field reference

## spec

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `hostname` | string | Yes | - | FQDN where the app will be accessible. Used to generate HTTPRoute and TLS certificate. Must match pattern `^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`. |
| `service` | [ServiceReference](#specservice) | Yes | - | The backend Kubernetes Service that receives traffic. |
| `routing` | [RoutingConfig](#specrouting) | No | - | Routing behavior including path rules and TLS. |
| `auth` | [AuthConfig](#specauth) | No | - | Authentication/authorization configuration. |
| `gateway` | string | No | `"public"` | Which shared Gateway to use. Valid values: `public`, `internal`. |

## spec.service

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `name` | string | Yes | - | Name of the Kubernetes Service in the same namespace. |
| `port` | int32 | Yes | - | Port number on the Service to route traffic to. Range: 1-65535. |

## spec.routing

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `routes` | [][RouteMatch](#specroutingroutes) | No | - | Path-based routing rules. If omitted, all traffic to the hostname is routed to the service. |
| `publicRoutes` | [][RouteMatch](#specroutingroutes) | No | - | Paths served without authentication even when `auth.enforceAtGateway` is true (this chart: `/healthz`, `/icon.svg`). Requires an operator version that supports it. |
| `tls` | [RoutingTLSConfig](#specroutingtls) | No | - | TLS certificate management configuration. |

### spec.routing.routes[]

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `pathPrefix` | string | Yes | - | Path prefix to match. Must start with `/`. Examples: `/`, `/api/v1`, `/dashboard`. |
| `pathType` | string | No | `"PathPrefix"` | How the path is matched. Values: `PathPrefix` (match prefix), `Exact` (exact match). |

### spec.routing.tls

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `enabled` | *bool | No | `true` | Whether to provision a TLS certificate via cert-manager and configure an HTTPS listener on the Gateway. When `false`, only HTTP listeners are used. |

## spec.auth

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `enabled` | bool | No | `false` | Whether to enforce OIDC authentication. |
| `provider` | string | No | `"keycloak"` | OIDC provider. Values: `keycloak`, `generic-oidc`. |
| `provisionClient` | *bool | No | `true` | Auto-provision an OIDC client in the provider. Only supported for `keycloak`. The operator creates the client and stores credentials in a Secret named `<name>-oidc-client`. The client ID follows the convention `<namespace>-<nebariapp-name>`. See the auth-flow documentation in the nebari-operator repo for the full secret structure. |
| `enforceAtGateway` | *bool | No | `true` | Create an Envoy Gateway SecurityPolicy for gateway-level auth. When `false`, the operator provisions the client and Secret but does NOT create a SecurityPolicy - the app handles OAuth natively. See the auth-flow documentation in the nebari-operator repo for wiring guidance. |
| `redirectURI` | string | No | `"/oauth2/callback"` | OAuth2 callback path. The full URL is `https://<hostname><redirectURI>`. |
| `clientSecretRef` | *string | No | - | Reference to a Secret containing `client-id` and `client-secret`. If omitted and `provisionClient` is true, the operator creates `<name>-oidc-client` with keys: `client-id`, `client-secret`, and optionally `issuer-url`. |
| `spaClient` | [SPAClientConfig](#specauthspaclient) | No | - | Provision a **public** PKCE client for a browser SPA (no client secret). Used with `enforceAtGateway: false` when a single-page app performs the OIDC login itself (e.g. via `keycloak-js`). |
| `scopes` | []string | No | `["openid", "profile", "email"]` | OIDC scopes to request during authentication. |
| `forwardAccessToken` | *bool | No | `false` | Forward the user's token to the backend (only valid with `enforceAtGateway`). This chart sets it so the API can verify the JWT. |
| `groups` | []string | No | - | Groups that have access. When specified, only users in these groups are authorized. Case-sensitive. Some operator versions use it for landing-page visibility only; see `adminGate.securityPolicy` below. |
| `issuerURL` | string | No | - | OIDC issuer URL. Required when `provider=generic-oidc`, ignored for `keycloak`. Example: `https://accounts.google.com`. |

### spec.auth.spaClient

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `enabled` | bool | No | `false` | Provision a public PKCE (no-secret) client for a browser SPA. |
| `clientId` | string | No | - | Client ID to provision. When empty, the operator generates one using the convention `<namespace>-<nebariapp-name>-spa`. The SPA must present this exact ID. |

> The Security Posture pack does **not** use the SPA client: login happens at the
> gateway (`enforceAtGateway: true`), and the API verifies the forwarded token itself.

## Status

The operator sets conditions on the NebariApp status to indicate readiness:

| Condition | Description |
|-----------|-------------|
| `RoutingReady` | HTTPRoute has been created and the Gateway is routing traffic. |
| `TLSReady` | TLS certificate is provisioned and the HTTPS listener is configured. |
| `AuthReady` | SecurityPolicy is created and OIDC client is available. Only set when `auth.enabled=true`. |
| `Ready` | Aggregate condition - all components are ready. |

### Condition Reasons

| Reason | Description |
|--------|-------------|
| `Available` | Resource is functioning correctly. |
| `Reconciling` | Reconciliation is in progress. |
| `ReconcileSuccess` | Reconciliation completed successfully. |
| `ValidationSuccess` | Validation passed successfully. |
| `NamespaceNotOptedIn` | Namespace is missing the `nebari.dev/managed=true` label. |
| `ServiceNotFound` | The referenced Service does not exist in the namespace. |
| `SecretNotFound` | The referenced Secret does not exist. |
| `GatewayNotFound` | The target Gateway does not exist. |
| `CertificateNotReady` | The cert-manager Certificate is not yet ready. |
| `Failed` | Reconciliation failed. |

## Namespace Opt-In

The namespace containing the NebariApp must be labeled for the operator to process it:

```bash
kubectl label namespace my-pack nebari.dev/managed=true
```

Without this label, the NebariApp will show `NamespaceNotOptedIn` and no resources will be created.
