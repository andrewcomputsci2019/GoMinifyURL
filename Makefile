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

help: ## Show this help.
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

all: clean tidy generate build

generate: proto oapi

# grpc gen
proto: proto_storage proto_admin
proto_storage:
	@echo "==== Generating grpc stubs from $(PROTO_STORAGE_SRC) ======"
	$(PROTOC) \
		--proto_path=proto \
		--go_out=$(PROTO_OUT) \
		--go_opt=paths=source_relative \
		--go-grpc_out=$(PROTO_OUT) \
		--go-grpc_opt=paths=source_relative \
		$(PROTO_STORAGE_SRC)
proto_admin:
	@echo "==== Generating grpc stubs from $(PROTO_ADMIN_SRC) ======"
		$(PROTOC) \
			--proto_path=proto \
			--go_out=$(PROTO_OUT) \
			--go_opt=paths=source_relative \
			--go-grpc_out=$(PROTO_OUT) \
			--go-grpc_opt=paths=source_relative \
			$(PROTO_ADMIN_SRC)

# OpenApi code gen stubs targets
oapi: oapi-shortener oapi-analytics oapi-admin

oapi-shortener:
	@echo "==== Generating Server Stubs from $(SHORTENER_SPEC) ===="
	@cd internal/api/shortener && \
	$(OAPI) -config $(SHORTENER_CONFIG) $(SHORTENER_SPEC)
	@echo "=== Code Gen complete ==="

oapi-analytics:
	@echo "==== Generating Server Stubs from $(ANALYTICS_SPEC) ===="
	@cd internal/api/analytics && \
	$(OAPI) -config $(ANALYTICS_CONFIG) $(ANALYTICS_SPEC)
	@echo "=== Code Gen complete ==="
oapi-admin:
	@echo "=== Generating Server Stubs from $(ADMIN_SPEC) ==="
	@cd internal/api/admin && \
	$(OAPI) -config $(ADMIN_CONFIG) $(ADMIN_SPEC)
	@echo "=== Code Gen complete ==="

tidy:
	@echo "=== Tidying Go mod ==="
	go mod tidy

build:
	@echo "=== building executables ==="
	#todo

clean:
	@echo "=== cleaning binaries ==="
	rm  bin/*
