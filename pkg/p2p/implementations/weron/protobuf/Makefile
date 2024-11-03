
all: go

go:
	protoc --go_out=. --go-grpc_out=. p2p.proto
	mv github.com/xaionaro-go/streamctl/pkg/p2p/implementations/weron/protobuf/go .
	rm -rf github.com