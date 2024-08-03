
all: go

go: player/go
	protoc --go_out=. --go-grpc_out=. streamd.proto

player/go:
	make -C player

