.PHONY: build test cover bench fuzz stress clean

build:
	go build -o running-quantile .

test:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; echo "gofmt needed"; exit 1; }
	go vet ./...
	go vet -tags stress ./...
	go test ./...

# cover fails when any function except main is below 100.0% statement coverage.
cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out
	@gaps=$$(go tool cover -func=coverage.out | grep -v '^total:' | grep -v '/main\.go:[0-9]*:[[:space:]]*main[[:space:]]' | grep -v '[[:space:]]100\.0%$$'); \
	test -z "$$gaps" || { echo "functions below 100.0% coverage:"; echo "$$gaps"; exit 1; }

bench:
	go test -run '^$$' -bench . -benchmem ./...

FUZZTIME ?= 60s

fuzz:
	go test -run '^$$' -fuzz '^FuzzExtractNumber$$' -fuzztime $(FUZZTIME) .
	go test -run '^$$' -fuzz '^FuzzTrackers$$' -fuzztime $(FUZZTIME) .
	go test -run '^$$' -fuzz '^FuzzProcess$$' -fuzztime $(FUZZTIME) .

stress:
	go test -count=1 -tags stress -run TestStress -timeout 90m -v .

clean:
	rm -f running-quantile coverage.out
