
ENABLE_VLC?=true
ENABLE_LIBAV?=true
FORCE_DEBUG?=false

WINDOWS_VLC_VERSION?=3.0.21

GOTAGS:=
ifeq ($(ENABLE_LIBAV), true)
	GOTAGS:=$(GOTAGS),with_libav
endif
ifeq ($(ENABLE_VLC), true)
	GOTAGS:=$(GOTAGS),with_libvlc
endif
ifeq ($(FORCE_DEBUG), true)
	GOTAGS:=$(GOTAGS),force_debug
endif
GOTAGS:=$(GOTAGS:,%=%)

ifneq ($(GOTAGS),)
	GOBUILD_FLAGS+=-tags $(GOTAGS)
	FYNEBUILD_FLAGS+=--tags $(GOTAGS)
endif

WINDOWS_CGO_FLAGS?=-I$(PWD)/3rdparty/amd64/windows/vlc-$(WINDOWS_VLC_VERSION)/sdk/include
WINDOWS_LINKER_FLAGS?=-L$(PWD)/3rdparty/amd64/windows/vlc-$(WINDOWS_VLC_VERSION)/sdk/lib -L$(PWD)/3rdparty/amd64/windows/ffmpeg-n7.0.2-19-g45ecf80f0e-win64-gpl-shared-7.0/lib
WINDOWS_PKG_CONFIG_PATH?=$(PWD)/3rdparty/amd64/windows/vlc-$(WINDOWS_VLC_VERSION)/sdk/lib/pkgconfig

all: streampanel-linux-amd64 streampanel-linux-arm64 streampanel-android streampanel-windows

3rdparty/amd64/windows:
	mkdir -p 3rdparty/amd64/windows
	sh -c 'cd 3rdparty/amd64/windows && wget https://get.videolan.org/vlc/$(WINDOWS_VLC_VERSION)/win64/vlc-$(WINDOWS_VLC_VERSION)-win64.7z && 7z x vlc-$(WINDOWS_VLC_VERSION)-win64.7z && rm -f vlc-$(WINDOWS_VLC_VERSION)-win64.7z'
	sh -c 'cd 3rdparty/amd64/windows && wget https://github.com/BtbN/FFmpeg-Builds/releases/download/autobuild-2024-09-29-12-53/ffmpeg-n7.0.2-19-g45ecf80f0e-win64-gpl-shared-7.0.zip && unzip ffmpeg-n7.0.2-19-g45ecf80f0e-win64-gpl-shared-7.0.zip && rm -f ffmpeg-n7.0.2-19-g45ecf80f0e-win64-gpl-shared-7.0.zip'

streampanel-linux-amd64: builddir
	CGO_ENABLED=1 CGO_LDFLAGS="-static" GOOS=linux GOARCH=amd64 go build $(GOBUILD_FLAGS) -o build/streampanel-linux-amd64 ./cmd/streampanel

streampanel-linux-arm64: builddir
	CGO_ENABLED=1 CGO_LDFLAGS="-static" GOOS=linux GOARCH=arm64 go build $(GOBUILD_FLAGS) -o build/streampanel-linux-arm64 ./cmd/streampanel

streampanel-macos-amd64: builddir
	CGO_ENABLED=1 CGO_LDFLAGS="-static" GOOS=darwin GOARCH=amd64 go build $(GOBUILD_FLAGS) -o build/streampanel-macos-amd64 ./cmd/streampanel

streampanel-macos-arm64: builddir
	CGO_ENABLED=1 CGO_LDFLAGS="-static" GOOS=darwin GOARCH=arm64 go build $(GOBUILD_FLAGS) -o build/streampanel-macos-arm64 ./cmd/streampanel

docker-termux-environment:
	cd 3rdparty/arm64/termux-packages && \
	./scripts/update-docker.sh

	cp 3rdparty/arm64/termux-patched-scripts/run-docker.sh \
	   3rdparty/arm64/termux-packages/scripts/run-docker.sh
	
	cd 3rdparty/arm64/termux-packages && \
	./scripts/run-docker.sh ./scripts/setup-android-sdk.sh

	cd 3rdparty/arm64/termux-packages && \
	./scripts/run-docker.sh ./build-package.sh -I ffmpeg

	cd 3rdparty/arm64/termux-packages && \
	./scripts/run-docker.sh ./build-package.sh -I libxxf86vm
	
	cd 3rdparty/arm64/termux-packages && \
	./scripts/run-docker.sh ./build-package.sh -I vlc

	cd 3rdparty/arm64/termux-packages && \
	./scripts/run-docker.sh sudo apt update

	cd 3rdparty/arm64/termux-packages && \
	./scripts/run-docker.sh sudo apt install -y golang-go

	cd 3rdparty/arm64/termux-packages && \
	./scripts/run-docker.sh go install fyne.io/fyne/v2/cmd/fyne@latest

dockerbuild-streampanel-android:
	cd 3rdparty/arm64/termux-packages && \
	./scripts/run-docker.sh make ENABLE_VLC=$(ENABLE_VLC) ENABLE_LIBAV=$(ENABLE_LIBAV) FORCE_DEBUG=$(FORCE_DEBUG) -C /project streampanel-android

streampanel-android: builddir
	cd cmd/streampanel && CGO_CFLAGS='-I /data/data/com.termux/files/usr/include/ -Wno-incompatible-function-pointer-types' PKG_CONFIG_PATH=/data/data/com.termux/files/usr/lib/pkgconfig/ ANDROID_NDK_HOME="$(shell ls -d /home/builder/lib/android-ndk-*)" PATH="${PATH}:${HOME}/go/bin" ANDROID_HOME="${HOME}"/Android/Sdk fyne package $(FYNEBUILD_FLAGS) -release -os android/arm64 && mv streampanel.apk ../../build/

streampanel-ios: builddir
	cd cmd/streampanel && fyne package $(GOBUILD_FLAGS) -release -os ios && mv streampanel.ipa ../../build/

streampanel-windows: 3rdparty/amd64/windows builddir
	PKG_CONFIG_PATH=$(WINDOWS_PKG_CONFIG_PATH) CGO_ENABLED=1 CGO_LDFLAGS="-static" CGO_CFLAGS="$(WINDOWS_CGO_FLAGS)" CC=x86_64-w64-mingw32-gcc GOOS=windows go build $(GOBUILD_FLAGS) -ldflags "-H windowsgui '-extldflags=$(WINDOWS_LINKER_FLAGS)'" -o build/windows-amd64/streampanel.exe ./cmd/streampanel/
	cp 3rdparty/amd64/windows/vlc-$(WINDOWS_VLC_VERSION)/*.dll build/windows-amd64/

streampanel-windows-debug: 3rdparty/amd64/windows builddir
	PKG_CONFIG_PATH=$(WINDOWS_PKG_CONFIG_PATH) CGO_ENABLED=1 CGO_LDFLAGS="-static" CGO_CFLAGS="$(WINDOWS_CGO_FLAGS)" CC=x86_64-w64-mingw32-gcc GOOS=windows go build $(GOBUILD_FLAGS) -ldflags "-a '-extldflags=$(WINDOWS_LINKER_FLAGS)'" -o build/windows-amd64/streampanel-debug.exe ./cmd/streampanel/
	cp 3rdparty/amd64/windows/vlc-$(WINDOWS_VLC_VERSION)/*.dll build/windows-amd64/

streamd-linux-amd64: builddir
	CGO_ENABLED=1 CGO_LDFLAGS="-static" GOOS=linux GOARCH=amd64 go build -o build/streamd-linux-amd64 ./cmd/streamd

streamcli-linux-amd64: builddir
	CGO_ENABLED=0 CGO_LDFLAGS="-static" GOOS=linux GOARCH=amd64 go build -o build/streamcli-linux-amd64 ./cmd/streamcli

streamcli-linux-arm64: builddir
	CGO_ENABLED=0 CGO_LDFLAGS="-static" GOOS=linux GOARCH=arm64 go build -o build/streamcli-linux-arm64 ./cmd/streamcli

builddir:
	mkdir -p build
