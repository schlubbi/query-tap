.PHONY: generate build test test-integration lint clean

generate:
	go generate ./...

build:
	go build -ldflags '-s -w' -o bin/querytap ./cmd/querytap

test:
	go test -v -race ./...

test-integration:
	go test -v -race -tags=integration ./...

lint:
	golangci-lint run

clean:
	rm -rf bin/
