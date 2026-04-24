.PHONY: build test int e2e lint fmt

build:
	go build -o bin/aios ./cmd/aios

test:
	go test ./...

int:
	go test ./test/integration/...

e2e:
	AIOS_E2E=1 go test -tags=e2e ./test/e2e/...

fmt:
	gofmt -w .

lint:
	go vet ./...
