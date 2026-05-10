# protoc-gen-streamd-shim

A protoc plugin that eliminates the 6+-files-per-RPC plumbing fan-out in
`pkg/streamd` by generating the server proxy and client wrapper for RPCs that
opt in via the annotation contract:

    option (streamd.proxy) = AUTO;

Place this option on an RPC whose reply message matches the shape
`{ string Error; <=1 scalar field; }`. The plugin then emits the matching
server-side `RequestX -> ReplyX` adapter (around the in-process `streamd`
implementation) and the client-side wrapper (request build, RPC dispatch,
error decode) into `<source>_streamd_proxy.pb.go`. RPCs without the
annotation are unaffected — migration is opt-in, per-RPC.

## Build & invoke

```sh
go build -o "$GOPATH/bin/protoc-gen-streamd-shim" ./cmd/protoc-gen-streamd-shim
protoc \
    --plugin=protoc-gen-streamd-shim="$GOPATH/bin/protoc-gen-streamd-shim" \
    --go_out=. --go-grpc_out=. --streamd-shim_out=. \
    streamd.proto streamd_options.proto
```

`pkg/streamd/grpc/Makefile` wires this up under `make go`.

## Status

Phase 1 lands the plugin and the `streamd.proxy` extension definition without
flipping any existing RPC. With zero AUTO annotations, the plugin emits an
empty `*_streamd_proxy.pb.go` per input — verifying the build pipeline before
any RPC is migrated.
