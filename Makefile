include Makefile-Arch.dep
PWD := $(shell pwd)
LDFLAGS := -ldflags="-s -w"
DEPLOY := bin
GC := go build
TORRXFER_OSARCH := GOOS=$(TORRXFER_KRNL) GOARCH=$(TORRXFER_ARCH)
PC := protoc
PREFIX := $(PWD)

PROTO_IN = ./proto
PROTOC_DIR = protoc-3.15.5-$(TORRXFER_OS)$(UNAME_P)
PROTO_SRC = $(PROTO_IN)/server.proto \

PROTO_OUT = rpc
PCFLAGS := --go_opt=paths=source_relative --go-grpc_opt=paths=source_relative -I$(PROTO_IN)
SERVER_SRC = ./cmd/server/main.go \

CLIENT_SRC = ./cmd/client/main.go \

TEST_SRC = ./cmd/... ./pkg/...

GO_TOOLS = golang.org/x/lint/golint \

all: deps lint torrxfer-server torrxfer-client

deps: tools vendor

protoc: protoc-install
	$(TORRXFER_OSARCH) go install google.golang.org/protobuf/cmd/protoc-gen-go@latest $(GO_DEPS)
	$(TORRXFER_OSARCH) go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest $(GO_DEPS)
	mkdir -p $(PROTO_OUT)
	$(PC) $(PCFLAGS) --go_out=$(PROTO_OUT) --go-grpc_out=$(PROTO_OUT) $(PROTO_SRC)
.PHONY: protoc

torrxfer-server: $(SERVER_SRC)
	$(TORRXFER_OSARCH) $(GC) $(LDFLAGS) -o $(DEPLOY)/$@ $^

torrxfer-client: $(CLIENT_SRC)
	$(TORRXFER_OSARCH) $(GC) $(LDFLAGS) -o $(DEPLOY)/$@ $^

vendor:
	$(TORRXFER_OSARCH) go mod vendor
.PHONY: vendor

tools:
	$(TORRXFER_OSARCH) go get -u $(GO_TOOLS) $(GO_DEPS)

lint:
	-golint $(TEST_SRC)
.PHONY: lint

test:
	$(TORRXFER_OSARCH) go test $(TEST_SRC)
.PHONY: test

PB_REL = https://github.com/protocolbuffers/protobuf/releases
protoc-install:
	curl -LO $(PB_REL)/download/v3.15.5/$(PROTOC_DIR).zip
	unzip -o  $(PROTOC_DIR).zip -d /tmp/build
	chmod +x /tmp/build/bin/protoc
	$(eval PC := /tmp/build/bin/protoc)

clean:
	rm -rf $(DEPLOY)
	rm -rf $(PROTO_OUT)
	rm -rf $(PROTOC_DIR).zip
	rm -rf /tmp/build/$(PROTOC_DIR)
	rm -rf vendor/