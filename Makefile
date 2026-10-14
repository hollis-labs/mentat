.PHONY: build build-full install test lint eval

build:
	go build -o mentat ./cmd/mentat

build-full:
	go build -tags treesitter -o mentat ./cmd/mentat

install:
	go install ./cmd/mentat

test:
	go test ./...

lint:
	go vet ./...

eval:
	go run ./cmd/mentat eval --queries eval/queries.yaml --baseline eval/baseline.json
