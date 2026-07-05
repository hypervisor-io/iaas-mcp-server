default: build

BINARY := iaas-mcp-server

.PHONY: build
build:
	go build -o $(BINARY) .

.PHONY: run
run: build
	./$(BINARY)

.PHONY: test
test:
	go test ./...

# Tri-sync (spec 17): refresh the vendored copy of the platform api-manifest.json
# from the Master repo. RUN THIS whenever the API contract changes so the
# manifest coverage gate (internal/tools/manifest_coverage_test.go) checks the
# current endpoint set. Master is private, so the manifest is vendored (a
# committed copy), not fetched in CI. Override MASTER_MANIFEST if Master lives
# elsewhere.
MASTER_MANIFEST ?= ../Master/api-manifest.json
.PHONY: sync-manifest
sync-manifest:
	cp $(MASTER_MANIFEST) internal/tools/testdata/api-manifest.json

.PHONY: vet
vet:
	go vet ./...

.PHONY: fmt
fmt:
	gofmt -w .

.PHONY: fmt-check
fmt-check:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "The following files are not gofmt'd:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

.PHONY: check
check: build vet fmt-check test
