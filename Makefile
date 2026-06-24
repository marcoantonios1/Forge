.PHONY: build run install clean

build:
	go build -o bin/forge ./cmd/forge

run: build
	./bin/forge

install: build
	sudo cp bin/forge /usr/local/bin/forge

clean:
	rm -rf bin
