.PHONY: build test

build:
	./scripts/build.sh

test:
	go test ./...
