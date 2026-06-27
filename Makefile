.PHONY: build build-go test run lint dashboard-build

dashboard-build:
	cd internal/dashboard/ui && npm install && npm run build

build: dashboard-build
	go build -o bin/zeep ./cmd/zeep

build-go:
	go build -o bin/zeep ./cmd/zeep

test:
	go test ./...

lint:
	go vet ./...

run:
	go run ./cmd/zeep
