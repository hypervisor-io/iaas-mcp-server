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
