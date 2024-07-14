
all: streampanel-linux-amd64 streampanel-linux-arm64 streampanel-android streampanel-windows

streampanel-linux-amd64: builddir
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o build/streampanel-linux-amd64 ./cmd/streampanel

streampanel-linux-arm64: builddir
	CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build -o build/streampanel-linux-arm64 ./cmd/streampanel

streampanel-macos-amd64: builddir
	CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build -o build/streampanel-macos-amd64 ./cmd/streampanel

streampanel-macos-arm64: builddir
	CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -o build/streampanel-macos-arm64 ./cmd/streampanel

streampanel-android: builddir
	cd cmd/streampanel && ANDROID_HOME=${HOME}/Android/Sdk fyne package -release -os android && mv streampanel.apk ../../build/

streampanel-ios: builddir
	cd cmd/streampanel && fyne package -release -os ios && mv streampanel.ipa ../../build/

streampanel-windows: builddir
	CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc GOOS=windows go build -ldflags "-H windowsgui" -o build/streampanel.exe ./cmd/streampanel/

streampanel-windows-debug: builddir
	CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc GOOS=windows go build -o build/streampanel-debug.exe ./cmd/streampanel/

streamd-linux-amd64: builddir
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o build/streamd-linux-amd64 ./cmd/streamd

streamcli-linux-amd64: builddir
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o build/streamcli-linux-amd64 ./cmd/streamcli

streamcli-linux-arm64: builddir
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o build/streamcli-linux-arm64 ./cmd/streamcli

builddir:
	mkdir -p build
