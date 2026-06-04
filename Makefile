.PHONY: build run clean

build:
	go build -o bin/forge ./cmd/forge

run: build
	./bin/forge

clean:
	rm -rf bin
