include Makefile-Arch.dep
# include Makefile-Protoc.dep
PWD := $(shell pwd)
LDFLAGS := -ldflags="-s -w"
DEPLOY := bin
GC := go build
TORRXFER_OSARCH := GOOS=$(TORRXFER_KRNL) GOARCH=$(TORRXFER_ARCH)
PC := $(HOME)/.local/bin/protoc
PREFIX := $(PWD)

PROTO_IN = ./proto
PROTOC_DIR = protoc-3.15.5-$(TORRXFER_OS)-$(UNAME_P)
PROTO_SRC = $(PROTO_IN)/server.proto \

PROTO_OUT = rpc
PCFLAGS := --go_opt=paths=source_relative --go-grpc_opt=paths=source_relative -I$(PROTO_IN)
SERVER_SRC = ./cmd/server/main.go \

CLIENT_SRC = ./cmd/client/main.go \

TEST_SRC = ./cmd/... ./pkg/...


all: deps lint torrxfer-server torrxfer-client

deps: proto vendor linter

proto: protoc $(PROTO_SRC)
	mkdir -p $(PROTO_OUT)
	$(PC) $(PCFLAGS) --go_out=$(PROTO_OUT) --go-grpc_out=$(PROTO_OUT) $(PROTO_SRC)

torrxfer-server: $(SERVER_SRC)
	$(TORRXFER_OSARCH) $(GC) $(LDFLAGS) -o $(DEPLOY)/$@ $^

torrxfer-client: $(CLIENT_SRC)
	$(TORRXFER_OSARCH) $(GC) $(LDFLAGS) -o $(DEPLOY)/$@ $^

vendor:
	$(TORRXFER_OSARCH) go mod vendor
.PHONY: vendor

linter:
	$(TORRXFER_OSARCH) go get -u golang.org/x/lint/golint

lint:
	golint $(TEST_SRC)
.PHONY: lint

PB_REL = "https://github.com/protocolbuffers/protobuf/releases"
protoc:
	curl -LO $(PB_REL)/download/v3.15.5/protoc-3.15.5-$(TORRXFER_OS)-$(UNAME_P).zip
	unzip -u  $(PROTOC_DIR).zip -d $(HOME)/.local

test:
	$(TORRXFER_OSARCH) go test $(TEST_SRC)
.PHONY: test


clean:
	rm -rf $(DEPLOY)
	rm -rf $(PROTO_OUT)
	rm -rf $(PROTOC_DIR).zip
	rm -rf $(HOME)/.local/$(PROTOC_DIR)
	rm -rf vendor/