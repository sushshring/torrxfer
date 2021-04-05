include Makefile-Arch.dep
PWD := $(shell pwd)
LDFLAGS := -ldflags="-s -w"
DEPLOY := bin
GC := go build
TORRXFER_OSARCH := GOOS=$(TORRXFER_KRNL) GOARCH=$(TORRXFER_ARCH)
PC := protoc
PREFIX := $(PWD)

PROTO_IN = ./proto
PROTO_SRC = $(PROTO_IN)/server.proto \

PROTO_OUT = rpc
PCFLAGS := --go_opt=paths=source_relative --go-grpc_opt=paths=source_relative -I$(PROTO_IN)
SERVER_SRC = ./cmd/server/main.go \

CLIENT_SRC = ./cmd/client/main.go \

TEST_SRC = ./cmd/... ./pkg/...

GO_TOOLS = golang.org/x/lint/golint \
			google.golang.org/protobuf/cmd/protoc-gen-go \
			google.golang.org/grpc/cmd/protoc-gen-go-grpc

all: deps lint torrxfer-server torrxfer-client

deps: tools proto vendor

proto: $(PROTO_SRC)
	mkdir -p $(PROTO_OUT)
	$(PC) $(PCFLAGS) --go_out=$(PROTO_OUT) --go-grpc_out=$(PROTO_OUT) $(PROTO_SRC)

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


clean:
	rm -rf $(DEPLOY)
	rm -rf $(PROTO_OUT)
	rm -rf $(PROTOC_DIR).zip
	rm -rf /tmp/build/$(PROTOC_DIR)
	rm -rf vendor/