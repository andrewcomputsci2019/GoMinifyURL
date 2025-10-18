SHELL = /bin/sh
# codegen tools
PROTOC = protoc
OAPI = go tool oapi-codegen

# Proto paths
PROTO_STORAGE_SRC := proto/storage.proto
PROTO_ADMIN_SRC := proto/admin.proto
PROTO_OUT := internal/proto

# OpenApi specs
SHORTENER_SPEC := ../../../openapi/urlMinfiy.yaml
ANALYTICS_SPEC := ../../../openapi/urlAnalytics.yaml
ADMIN_SPEC	   := ../../../openapi/adminAPI.yaml

# OAPI-CodeGen config files
SHORTENER_CONFIG := oapi-codegen.yaml
ANALYTICS_CONFIG := oapi-codegen.yaml
ADMIN_CONFIG	 := oapi-codegen.yaml

# Targets
.PHONY: all proto oapi tidy generate build clean

help: ## Display available make targets with descriptions
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Available targets:"
	@grep -E '^[a-zA-Z0-9_.-]+:.*?## .*$$' $(MAKEFILE_LIST) \
	| sort \
	| awk 'BEGIN {FS = ":.*?## "}; \
		{printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "Common workflows:"
	@echo "  make all           – Clean, tidy, generate code, and build everything"
	@echo "  make proto         – Generate all gRPC stubs"
	@echo "  make oapi          – Generate all OpenAPI stubs"
	@echo "  make build-admin   – Build only the Admin binary"
	@echo "  make clean         – Remove built binaries"
	@echo ""

all: clean tidy generate build

generate: proto oapi

# grpc gen
proto: proto_storage proto_admin
proto_storage: ## generates the storage protobuf/grpc stubs
	@echo "==== Generating grpc stubs from $(PROTO_STORAGE_SRC) ======"
	$(PROTOC) \
		--proto_path=proto \
		--go_out=$(PROTO_OUT)/storage \
		--go_opt=paths=source_relative \
		--go-grpc_out=$(PROTO_OUT)/storage \
		--go-grpc_opt=paths=source_relative \
		$(PROTO_STORAGE_SRC)
proto_admin: ## generates the admin protobuf/grpc stubs
	@echo "==== Generating grpc stubs from $(PROTO_ADMIN_SRC) ======"
		$(PROTOC) \
			--proto_path=proto \
			--go_out=$(PROTO_OUT)/admin \
			--go_opt=paths=source_relative \
			--go-grpc_out=$(PROTO_OUT)/admin \
			--go-grpc_opt=paths=source_relative \
			$(PROTO_ADMIN_SRC)

# OpenApi code gen stubs targets
oapi: oapi-shortener oapi-analytics oapi-admin

oapi-shortener: ## generates the open-api url shortener stubs
	@echo "==== Generating Server Stubs from $(SHORTENER_SPEC) ===="
	@cd internal/api/shortener && \
	$(OAPI) -config $(SHORTENER_CONFIG) $(SHORTENER_SPEC)
	@echo "=== Code Gen complete ==="

oapi-analytics: ## generates the open-api analytics stubs
	@echo "==== Generating Server Stubs from $(ANALYTICS_SPEC) ===="
	@cd internal/api/analytics && \
	$(OAPI) -config $(ANALYTICS_CONFIG) $(ANALYTICS_SPEC)
	@echo "=== Code Gen complete ==="
oapi-admin: ## generates the open-api admin stubs
	@echo "=== Generating Server Stubs from $(ADMIN_SPEC) ==="
	@cd internal/api/admin && \
	$(OAPI) -config $(ADMIN_CONFIG) $(ADMIN_SPEC)
	@echo "=== Code Gen complete ==="

tidy: ## runs go mod tidy for the package
	@echo "=== Tidying Go mod ==="
	go mod tidy

# todo add more build steps
build: build-admin

build-admin: ## builds the admin binary
	@echo "=== Building Admin Executable ==="
	go build -o ./bin/admin ./cmd/admin/main.go
	@echo "=== Finished Building Admin Executable ==="


clean: ## removes all binaries inside the build dir, should be run before all build calls
	@echo "=== cleaning binaries ==="
	rm  bin/*
