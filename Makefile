.PHONY: build build-go test run lint dashboard-build helm-lint helm-template helm-dry-run

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

helm-lint:
	helm lint charts/zeep-orbit/

helm-template:
	helm template zeep-orbit charts/zeep-orbit/ \
		--set secrets.databaseUrl=postgres://user:pass@host:5432/db \
		--set 'secrets.apps.billing.jwtSecret=change-me' \
		--set 'secrets.apps.inventory.jwtSecret=change-me'

helm-dry-run:
	helm upgrade --install zeep-orbit charts/zeep-orbit/ \
		--namespace zeep --create-namespace \
		--dry-run \
		--set secrets.databaseUrl=postgres://user:pass@host:5432/db \
		--set 'secrets.apps.billing.jwtSecret=change-me' \
		--set 'secrets.apps.inventory.jwtSecret=change-me'
