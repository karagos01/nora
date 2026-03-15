.PHONY: all build server client client-windows clean dev-server test test-verbose docker

BUILD_NUM := $(shell grep -o '"build": [0-9]*' version.json | grep -o '[0-9]*')
LDFLAGS := -X main.version=$(BUILD_NUM)

all: build

build: server client

# Go server
server:
	cd server && go build -o nora .

# Nativní klient (Go + Gio, vyžaduje CGO kvůli malgo)
client:
	cd client-native && go build -ldflags '$(LDFLAGS)' -o nora-native .

# Windows cross-compile (MinGW)
client-windows:
	cd client-native && CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc GOOS=windows GOARCH=amd64 go build -ldflags '$(LDFLAGS)' -o ../NORA-windows/NORA.exe .

# Development
dev-server:
	cd server && go run .

# Testy
test:
	cd server && go test ./...

test-verbose:
	cd server && go test -v ./...

test-client:
	cd client-native && go test ./...

# Vyčištění
clean:
	rm -f server/nora
	rm -f client-native/nora-native

# Docker (server only)
docker:
	docker build -t nora .
