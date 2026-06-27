.PHONY: build test run lint

build:
	go build -o bin/zeep ./cmd/zeep

test:
	go test ./...

lint:
	go vet ./...

run:
	go run ./cmd/zeep
