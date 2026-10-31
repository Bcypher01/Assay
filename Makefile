GOLANGCI_VERSION := v2.1.6
TOOLS := $(shell go env GOPATH)/bin

.PHONY: build test race bench cover lint fmt proto tools bootstrap clean \
        web-install web-build web-start compose-up compose-down

build:
	go build -trimpath -o bin/assay ./cmd/assay
	go build -trimpath -o bin/pipeline ./cmd/pipeline
	go build -trimpath -o bin/event-pipeline ./cmd/event-pipeline

test:
	go test ./...

race:
	go test -race ./...

bench:
	go test -run=XXX -bench=. -benchmem ./internal/...

cover:
	go test -coverprofile=cover.out ./... && go tool cover -func=cover.out

# Fails on any unformatted file: gofmt -l alone exits 0 and CI would not notice.
lint:
	@test -z "$$(gofmt -l . | grep -v '^gen/' | grep -v node_modules | tee /dev/stderr)" || \
		{ echo "gofmt: the files above are not formatted; run make fmt"; exit 1; }
	go vet ./...
	$(TOOLS)/golangci-lint run ./...

fmt:
	$(TOOLS)/golangci-lint fmt ./...

proto:
	PATH="$(TOOLS):$$PATH" $(TOOLS)/buf generate proto

# Pinned developer tools. CI installs the same versions.
tools:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)
	go install github.com/bufbuild/buf/cmd/buf@v1.50.0

bootstrap: tools
	go mod download
	npm --prefix web ci

clean:
	rm -rf bin dist cover.out

web-install:
	npm --prefix web ci

web-build:
	npm --prefix web run build

web-start:
	npm --prefix web run start

compose-up:
	test -f deploy/.env || cp deploy/.env.example deploy/.env
	docker compose up --build

compose-down:
	docker compose down
