.PHONY: build test cover bench fuzz clean

build:
	go build -o running-quantile .

test:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; echo "gofmt needed"; exit 1; }
	go vet ./...
	go vet -tags stress ./...
	go test ./...

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

bench:
	go test -run '^$$' -bench . -benchmem ./...

FUZZTIME ?= 60s

fuzz:
	go test -run '^$$' -fuzz '^FuzzExtractNumber$$' -fuzztime $(FUZZTIME) .
	go test -run '^$$' -fuzz '^FuzzTrackers$$' -fuzztime $(FUZZTIME) .
	go test -run '^$$' -fuzz '^FuzzProcess$$' -fuzztime $(FUZZTIME) .

clean:
	rm -f running-quantile coverage.out
