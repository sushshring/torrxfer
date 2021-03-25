include Makefile-Arch.dep
PWD := $(shell pwd)
LDFLAGS := -ldflags="-s -w"
DEPLOY := bin
GC := go build
TORRXFER_OSARCH := GOOS=$(TORRXFER_OS) GOARCH=$(TORRXFER_ARCH)
SERVER_SRC = ./cmd/server/main.go
CLIENT_SRC = ./cmd/client/main.go

all: torrxfer-server torrxfer-client

torrxfer-server: $(SERVER_SRC)
	$(TORRXFER_OSARCH) $(GC) $(LDFLAGS) -o $(DEPLOY)/$@ $^

torrxfer-client: $(CLIENT_SRC)
	$(TORRXFER_OSARCH) $(GC) $(LDFLAGS) -o $(DEPLOY)/$@ $^

vendor:
	$(TORRXFER_OSARCH) go mod vendor
.PHONY: vendor

lint:
	golint ./...
.PHONY: lint

clean:
	rm -rf $(DEPLOY)