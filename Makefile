.PHONY: build test bench lint proto race cover clean

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

lint:
	go vet ./...
	gofmt -l .

proto:
	buf generate

clean:
	rm -rf bin dist cover.out
