# GoMinifyURL

> 🛑 This project is not at a stable release APIs are bound to change and bugs should be expected,
> further, guides on usage, building, and hosting are not fully complete.

## A Privacy-Conscious URL Shortening Platform

GoMinifyURL is an open-source URL shortening service built in Go with a focus on privacy, user ownership, and transparency. It supports short link creation, redirection, and detailed analytics — all authenticated using Keycloak.

---

## Features

- 🔗 Shorten long URLs into concise short links
- 📊 Per-user analytics including:
    - Click counts
    - Geo-based access statistics
    - Referral graphs
    - Top URLs
- Authenticated via Keycloak (OAuth2 / OpenID Connect)
- Modular API structure with OpenAPI 3.0.4 + gRPC
- Easily deployable as two microservices:
    - `redirect-web`: public-facing redirect + shortener
    - `analytics-web`: authenticated stats API
    - `storage-service`: handles storing information and caching
- easily scalable through admin panel

---

## Getting Started

### Prerequisites

- Go 1.22+
- `protoc` (for gRPC)
- `oapi-codegen` (for OpenAPI codegen)
- Docker (optional, for deployment)
- Keycloak instance (or another OIDC provider)

### Quick Build

```bash
make generate     # Generate proto and OpenAPI stubs
make build        # Build redirect and analytics services
```

### Running the Services

```bash
./bin/redirect-web     # Serves redirect and /shrink endpoint
./bin/analytics-web    # Serves user-authenticated analytics API
```

Each service reads config from environment variables (e.g. DB, Keycloak).

---

## API Overview

OpenAPI specifications:

- `openapi/urlMinfiy.yaml` – Shortener + redirect API
- `openapi/urlAnalytics.yaml` – User analytics API

Generated server stubs live under:

- `api/shortener/`
- `api/analytics/`

All endpoints requiring user ownership enforce authentication via Keycloak.

---

## 🔬Development

### Generate Code

```bash
make proto           # Regenerates gRPC code (internal/proto)
make oapi            # Regenerates OpenAPI Go stubs
```

### Run Tests

```bash
go test ./...
```

---

## 🛡️ License

This project is licensed under the [GNU Affero General Public License v3.0](LICENSE).

> ⚠️ If you deploy this software as part of a public or commercial service (e.g., web-hosted), you must release your source code, including any modifications, under the same license.

---

