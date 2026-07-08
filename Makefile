OAPI_CODEGEN ?= oapi-codegen
OAPI_CODEGEN_VERSION := v2.7.1
SPEC := doc/api/openapi.yaml
CODEGEN_CFG := doc/api/oapi-codegen.yaml
GENERATED := internal/api/openapi.gen.go

.PHONY: help
help:
	@printf '%s\n' 'Targets:'
	@printf '%s\n' '  make install-tools   Install oapi-codegen (pinned to $(OAPI_CODEGEN_VERSION))'
	@printf '%s\n' '  make generate        Generate Go DTOs and server stubs from $(SPEC)'
	@printf '%s\n' '  make verify          Fail if $(GENERATED) is stale vs. the spec (drift check)'
	@printf '%s\n' '  make test            Run go test ./...'
	@printf '%s\n' '  make build           Build cmd/ec and cmd/ecdb'
	@printf '%s\n' '  make run             Run the skeleton server'
	@printf '%s\n' '  make clean           Remove local build products and generated code'

.PHONY: install-tools
install-tools:
	go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@$(OAPI_CODEGEN_VERSION)

.PHONY: generate
generate:
	@mkdir -p $(dir $(GENERATED))
	$(OAPI_CODEGEN) --config $(CODEGEN_CFG) $(SPEC)

# Drift check: regenerate and fail if the committed output differs. The spec is
# the source of truth (see doc/api/conventions.md); CI runs this so a spec change
# without a matching `make generate` is caught.
.PHONY: verify
verify: generate
	@if ! git diff --quiet -- $(GENERATED); then \
		echo 'error: $(GENERATED) is out of date; run `make generate` and commit the result.' >&2; \
		git --no-pager diff -- $(GENERATED) >&2; \
		exit 1; \
	fi

.PHONY: test
test:
	go test ./...

.PHONY: build
build:
	mkdir -p dist/local
	go build -o dist/local/ec   ./cmd/ec
	go build -o dist/local/ecdb ./cmd/ecdb

.PHONY: run
run:
	go run ./cmd/ec

.PHONY: clean
clean:
	rm -rf dist/local/ec dist/local/ecdb
	rm -f $(GENERATED)
