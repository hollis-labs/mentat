.PHONY: build build-full install test lint eval generate pins scan

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
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.11.4 run --max-issues-per-linter=0 --max-same-issues=0

eval:
	@test -n "$$MENTAT_DATA_DIR" || { echo "set MENTAT_DATA_DIR to a scratch copy; eval must not run on the live data"; exit 1; }
	go run ./cmd/mentat eval --queries eval/queries.yaml --baseline eval/baseline.json

generate:
	go run github.com/hollis-labs/substrate/cmd/substrate generate -f cmd/mentat/substrate.yaml

pins:
	go run github.com/hollis-labs/substrate/cmd/substrate pins check --app .

# The tree must be clean; the history gate returns after the program-wide history rewrite pass.
scan:
	python3 scripts/scan-public --tree
