
PROTOC_GEN_STREAMD_SHIM?=$(GOPATH)/bin/protoc-gen-streamd-shim
ifeq ($(GOPATH),)
GOPATH:=$(shell go env GOPATH)
PROTOC_GEN_STREAMD_SHIM:=$(GOPATH)/bin/protoc-gen-streamd-shim
endif

all: go

# `streamd-shim` builds the protoc plugin and generates the streamd-shim outputs
# alongside the standard protoc-gen-go / protoc-gen-go-grpc artifacts. Phase 1
# emits empty *_streamd_proxy.pb.go files; Phase 2+ will emit the server proxy
# and client wrapper for RPCs annotated with option (streamd.proxy) = AUTO;.
go: player/go streamd-shim-plugin
	protoc \
		--plugin=protoc-gen-streamd-shim=$(PROTOC_GEN_STREAMD_SHIM) \
		--go_out=. \
		--go-grpc_out=. \
		--streamd-shim_out=. \
		streamd.proto streamd_options.proto

streamd-shim-plugin:
	cd ../../../ && go build -o $(PROTOC_GEN_STREAMD_SHIM) ./cmd/protoc-gen-streamd-shim/

player/go:
	make -C player go

cpp: player/cpp
	mkdir -p cpp
	protoc --grpc_out=./cpp/ --plugin=protoc-gen-grpc="$(shell which grpc_cpp_plugin)" streamd.proto
	protoc --cpp_out=./cpp/ streamd.proto

player/cpp:
	make -C player cpp


