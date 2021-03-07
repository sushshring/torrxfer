LDFLAGS := -ldflags="-s -w"
DEPLOY := bin

lint:
	golangci-lint run ./...
.PHONY: lint

linux:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/server ./cmd/server
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/server ./cmd/client
.PHONY: linux

win:
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o bin/server ./cmd/server
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o bin/server ./cmd/client
.PHONY: win