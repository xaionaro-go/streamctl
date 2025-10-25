
all: go

go:
	protoc --go_out=. --go-grpc_out=. streamcontrol.proto
	mv github.com/xaionaro-go/streamctl/pkg/streamcontrol/protobuf/go .
	rm -rf github.com

cpp:
	mkdir -p cpp
	protoc --grpc_out=./cpp/ --plugin=protoc-gen-grpc="$(shell which grpc_cpp_plugin)" streamcontrol.proto
	protoc --cpp_out=./cpp/ streamcontrol.proto
