.PHONY: run
run:
	go run main.go

.PHONY: build
build:
	mkdir -p build/
	go build -o build/gaia

.PHONY: install
install: build
	mkdir -p ~/bin
	cp build/gaia ~/bin/install
