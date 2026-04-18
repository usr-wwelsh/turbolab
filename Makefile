BIN := turbolab
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build web clean

build: web
	go build -ldflags "-X main.version=$(VERSION)" -o $(BIN) .

web:
	cd web && npm run build

clean:
	rm -f $(BIN)
	rm -rf web/dist
