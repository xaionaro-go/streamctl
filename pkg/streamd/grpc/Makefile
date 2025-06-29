
all: go

go: player/go
	protoc --go_out=. --go-grpc_out=. streamd.proto

player/go:
	make -C player go

cpp: player/cpp
	mkdir -p cpp
	protoc --grpc_out=./cpp/ --plugin=protoc-gen-grpc="$(shell which grpc_cpp_plugin)" streamd.proto
	protoc --cpp_out=./cpp/ streamd.proto

player/cpp:
	make -C player cpp


