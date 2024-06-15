
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
	cd cmd/streampanel && ANDROID_HOME=${HOME}/Android/Sdk fyne package -release -os android -o ../../build/streampanel.apk

streampanel-ios: builddir
	cd cmd/streampanel && fyne package -release -os ios -o ../../build/streampanel.ipa

streampanel-windows: builddir
	CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc GOOS=windows go build -o build/streampanel.exe ./cmd/streampanel/

builddir:
	mkdir -p build
