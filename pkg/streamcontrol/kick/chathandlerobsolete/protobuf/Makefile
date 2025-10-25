
all: go

go:
	protoc --go_out=. --go-grpc_out=. chathandlerobsolete.proto
	mv github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick/chathandlerobsolete/protobuf/go .
	rm -rf github.com

cpp:
	mkdir -p cpp
	protoc --grpc_out=./cpp/ --plugin=protoc-gen-grpc="$(shell which grpc_cpp_plugin)" chathandlerobsolete.proto
	protoc --cpp_out=./cpp/ chathandlerobsolete.proto
