
ENABLE_VLC?=true
ENABLE_LIBAV?=true
FORCE_DEBUG?=false

WINDOWS_VLC_VERSION?=3.0.21
ANDROID_NDK_VERSION?=r27b

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

GOBUILD_FLAGS?=-buildvcs=true
ifneq ($(GOTAGS),)
	GOBUILD_FLAGS+=-tags=$(GOTAGS)
	FYNEBUILD_FLAGS+=--tags $(GOTAGS)
endif

GOPATH?=$(shell go env GOPATH)

WINDOWS_CGO_FLAGS?=-I$(PWD)/3rdparty/amd64/windows/vlc-$(WINDOWS_VLC_VERSION)/sdk/include
WINDOWS_EXTLINKER_FLAGS?=-L$(PWD)/3rdparty/amd64/windows/vlc-$(WINDOWS_VLC_VERSION)/sdk/lib -L$(PWD)/3rdparty/amd64/windows/ffmpeg-n7.0-21-gfb8f0ea7b3-win64-gpl-shared-7.0/lib
WINDOWS_PKG_CONFIG_PATH?=$(PWD)/3rdparty/amd64/windows/vlc-$(WINDOWS_VLC_VERSION)/sdk/lib/pkgconfig

GIT_COMMIT?=$(shell git rev-list -1 HEAD)
VERSION_STRING?=$(shell git rev-list -1 HEAD)
BUILD_DATE_STRING?=$(shell date +%s)

LINKER_FLAGS?=-X=github.com/xaionaro-go/streamctl/pkg/buildvars.GitCommit=$(GIT_COMMIT) -X=github.com/xaionaro-go/streamctl/pkg/buildvars.Version=$(VERSION_STRING) -X=github.com/xaionaro-go/streamctl/pkg/buildvars.BuildDateString=$(BUILD_DATE_STRING) -X=github.com/xaionaro-go/streamctl/pkg/buildvars.TwitchClientID=$(TWITCH_CLIENT_ID) -X=github.com/xaionaro-go/streamctl/pkg/buildvars.TwitchClientSecret=$(TWITCH_CLIENT_SECRET)

LINKER_FLAGS_ANDROID?=$(LINKER_FLAGS)
LINKER_FLAGS_DARWIN?=$(LINKER_FLAGS)
LINKER_FLAGS_LINUX?=$(LINKER_FLAGS)
LINKER_FLAGS_WINDOWS?=$(LINKER_FLAGS) '-extldflags=$(WINDOWS_EXTLINKER_FLAGS)'

all: streampanel-linux-amd64 streampanel-linux-arm64 streampanel-android-arm64 streampanel-windows

$(GOPATH)/bin/pkg-config-wrapper:
	go install github.com/xaionaro-go/pkg-config-wrapper@5dd443e6c18336416c49047e2ba0002e26a85278

3rdparty/arm64/android-ndk-$(ANDROID_NDK_VERSION):
	mkdir -p 3rdparty/arm64
	cd 3rdparty/arm64 && wget https://dl.google.com/android/repository/android-ndk-$(ANDROID_NDK_VERSION)-linux.zip && unzip android-ndk-$(ANDROID_NDK_VERSION)-linux.zip && rm -f android-ndk-$(ANDROID_NDK_VERSION)-linux.zip

3rdparty/arm64/termux:
	mkdir -p 3rdparty/arm64/termux
	cd 3rdparty/arm64/termux && \
	for PACKAGE in \
		vlc_3.0.21-1_aarch64.deb \
		vlc-static_3.0.21-1_aarch64.deb \
		ffmpeg_6.1.2_aarch64.deb \
		ffmpeg-static_6.1.2_aarch64.deb \
		libvpx-static_1:1.14.1_aarch64.deb \
		libwebp-static_1.4.0-rc1-0_aarch64.deb \
	; do \
		wget https://github.com/xaionaro/termux-prebuilt-packages/raw/refs/heads/main/$$PACKAGE && ar x $$PACKAGE && tar -xJvf data.tar.xz && rm -f data.tar.xz control.tar.xz debian-binary $$PACKAGE; \
	done

3rdparty/amd64/windows/ready:
	mkdir -p 3rdparty/amd64/windows
	sh -c 'cd 3rdparty/amd64/windows && wget https://get.videolan.org/vlc/$(WINDOWS_VLC_VERSION)/win64/vlc-$(WINDOWS_VLC_VERSION)-win64.7z && 7z -y x vlc-$(WINDOWS_VLC_VERSION)-win64.7z && rm -f vlc-$(WINDOWS_VLC_VERSION)-win64.7z'
	sh -c 'cd 3rdparty/amd64/windows && wget https://github.com/BtbN/FFmpeg-Builds/releases/download/autobuild-2024-04-30-12-51/ffmpeg-n7.0-21-gfb8f0ea7b3-win64-gpl-shared-7.0.zip && unzip -o ffmpeg-n7.0-21-gfb8f0ea7b3-win64-gpl-shared-7.0.zip && rm -f ffmpeg-n7.0-21-gfb8f0ea7b3-win64-gpl-shared-7.0.zip'
	mkdir 3rdparty/amd64/windows/mpv
	sh -c 'cd 3rdparty/amd64/windows/mpv && wget https://github.com/shinchiro/mpv-winbuild-cmake/releases/download/20241025/mpv-x86_64-20241025-git-5c59f8a.7z && 7z -y x mpv-x86_64-20241025-git-5c59f8a.7z && rm -f mpv-x86_64-20241025-git-5c59f8a.7z'
	touch 3rdparty/amd64/windows/ready

windows-builddir: build/streampanel-windows-amd64

build/streampanel-windows-amd64:
	mkdir -p build/streampanel-windows-amd64

windows-debug-builddir: build/streampanel-windows-debug-amd64

build/streampanel-windows-debug-amd64:
	mkdir -p build/streampanel-windows-debug-amd64

windows-deps: build/streampanel-windows-amd64/libvlc.dll build/streampanel-windows-amd64/avdevice-61.dll build/streampanel-windows-amd64/mpv/mpv.exe

windows-debug-deps: build/streampanel-windows-debug-amd64/libvlc.dll build/streampanel-windows-debug-amd64/avdevice-61.dll build/streampanel-windows-debug-amd64/mpv/mpv.exe

build/streampanel-windows-amd64/libvlc.dll: windows-builddir 3rdparty/amd64/windows/ready
	cp -av 3rdparty/amd64/windows/vlc-$(WINDOWS_VLC_VERSION)/*.dll 3rdparty/amd64/windows/vlc-$(WINDOWS_VLC_VERSION)/plugins build/streampanel-windows-amd64/

build/streampanel-windows-amd64/avdevice-61.dll: windows-builddir 3rdparty/amd64/windows/ready
	cp -av 3rdparty/amd64/windows/ffmpeg*/bin/*.dll build/streampanel-windows-amd64/

build/streampanel-windows-amd64/mpv/mpv.exe: windows-builddir 3rdparty/amd64/windows/ready
	mkdir -p build/streampanel-windows-amd64/mpv
	cp -av 3rdparty/amd64/windows/mpv/*.exe 3rdparty/amd64/windows/mpv/*.dll build/streampanel-windows-amd64/mpv/

build/streampanel-windows-debug-amd64/libvlc.dll: windows-debug-builddir 3rdparty/amd64/windows/ready
	cp -av 3rdparty/amd64/windows/vlc-$(WINDOWS_VLC_VERSION)/*.dll 3rdparty/amd64/windows/vlc-$(WINDOWS_VLC_VERSION)/plugins build/streampanel-windows-debug-amd64/

build/streampanel-windows-debug-amd64/avdevice-61.dll: windows-debug-builddir 3rdparty/amd64/windows/ready
	cp -av 3rdparty/amd64/windows/ffmpeg*/bin/*.dll build/streampanel-windows-debug-amd64/

build/streampanel-windows-debug-amd64/mpv/mpv.exe: windows-debug-builddir 3rdparty/amd64/windows/ready
	mkdir -p build/streampanel-windows-debug-amd64/mpv
	cp -av 3rdparty/amd64/windows/mpv/*.exe 3rdparty/amd64/windows/mpv/*.dll build/streampanel-windows-debug-amd64/mpv/

streampanel-linux-amd64: builddir
	$(eval INSTALL_DEST?=build/streampanel-linux-amd64)
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build $(GOBUILD_FLAGS) -ldflags "$(LINKER_FLAGS_LINUX)" -o "$(INSTALL_DEST)" ./cmd/streampanel

streampanel-linux-arm64: builddir
	$(eval INSTALL_DEST?=build/streampanel-linux-arm64)
	CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build $(GOBUILD_FLAGS) -ldflags "$(LINKER_FLAGS_LINUX)" -o "$(INSTALL_DEST)" ./cmd/streampanel

streampanel-macos-amd64: builddir
	$(eval INSTALL_DEST?=build/streampanel-macos-amd64)
	CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build $(GOBUILD_FLAGS) -ldflags "$(LINKER_FLAGS_DARWIN)" -o "$(INSTALL_DEST)" ./cmd/streampanel

streampanel-macos-arm64: builddir
	$(eval INSTALL_DEST?=build/streampanel-macos-arm64)
	CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build $(GOBUILD_FLAGS) -ldflags "$(LINKER_FLAGS_DARWIN)" -o "$(INSTALL_DEST)" ./cmd/streampanel

DOCKER_IMAGE?=xaionaro2/streampanel-android-builder
DOCKER_CONTAINER_NAME?=streampanel-android-builder

dockerbuilder-android-arm64:
	docker pull  $(DOCKER_IMAGE)
	docker start $(DOCKER_IMAGE) >/dev/null 2>&1 || \
		docker run \
			--detach \
			--init \
			--name $(DOCKER_CONTAINER_NAME) \
			--volume ".:/project" \
			--tty \
			$(DOCKER_IMAGE) >/dev/null 2>&1 || /bin/true

dockerbuild-streampanel-android-arm64: dockerbuilder-android-arm64
	docker exec $(DOCKER_CONTAINER_NAME) make ENABLE_VLC="$(ENABLE_VLC)" ENABLE_LIBAV="$(ENABLE_LIBAV)" FORCE_DEBUG="$(FORCE_DEBUG)" -C /project streampanel-android-arm64-in-docker

checkconfig-android-in-docker:
	@if [ "$(ENABLE_VLC)" != 'false' ]; then \
		echo "VLC is not supported for Android builds, yet, please disable it with ENABLE_VLC=false."; \
	    exit 1; \
	fi

checkconfig-android:
	@if [ "$(ENABLE_VLC)" != 'false' ]; then \
		echo "VLC is not supported for Android builds, yet, please disable it with ENABLE_VLC=false."; \
	    exit 1; \
	fi
	@if [ "$(ENABLE_LIBAV)" != 'false' ]; then \
		echo "Building with LibAV support is not supported outside of the docker container yet. Please either disable LibAV with ENABLE_LIBAV=false or use `make dockerbuild-streampanel-android-arm64` instead."; \
		exit 1; \
	fi

streampanel-android-arm64-in-docker: build-streampanel-android-arm64-in-docker check-streampanel-android-arm64-static-cgo

build-streampanel-android-arm64-in-docker: checkconfig-android-in-docker builddir $(GOPATH)/bin/pkg-config-wrapper
	go mod tidy
	git config --global --add safe.directory /project
	$(eval ANDROID_NDK_HOME=$(shell ls -d /home/builder/lib/android-ndk-*))
	cd cmd/streampanel && \
		PKG_CONFIG_WRAPPER_LOG='/tmp/pkg_config_wrapper.log' \
		PKG_CONFIG_WRAPPER_LOG_LEVEL='trace' \
		PKG_CONFIG_LIBS_FORCE_STATIC='libav*,libvlc' \
		PKG_CONFIG_ERASE="-fopenmp=*,-landroid" \
		PKG_CONFIG='$(GOPATH)/bin/pkg-config-wrapper' \
		PKG_CONFIG_PATH='/data/data/com.termux/files/usr/lib/pkgconfig' \
		CGO_CFLAGS='-I$(ANDROID_NDK_HOME)/toolchains/llvm/prebuilt/linux-x86_64/sysroot/usr/include/ -I/data/data/com.termux/files/usr/include -Wno-incompatible-function-pointer-types -Wno-unused-result -Wno-xor-used-as-pow' \
		CGO_LDFLAGS='-ldl -lc -L$(ANDROID_NDK_HOME)/toolchains/llvm/prebuilt/linux-x86_64/sysroot/usr/lib/ -L/data/data/com.termux/files/usr/lib' \
		ANDROID_NDK_HOME="$(ANDROID_NDK_HOME)" \
		PATH="${PATH}:${HOME}/go/bin" \
		GOFLAGS="$(GOBUILD_FLAGS) -ldflags=$(shell echo ${LINKER_FLAGS_ANDROID} | tr " " ",")" \
		fyne package $(FYNEBUILD_FLAGS) -release -os android/arm64 && mv streampanel.apk ../../build/streampanel-arm64.apk

streampanel-android-arm64-static-cgo: build-streampanel-android-arm64-static-cgo check-streampanel-android-arm64-static-cgo

build-streampanel-android-arm64-static-cgo: builddir $(GOPATH)/bin/pkg-config-wrapper 3rdparty/arm64/android-ndk-$(ANDROID_NDK_VERSION) 3rdparty/arm64/termux
	$(eval ANDROID_NDK_HOME=$(PWD)/3rdparty/arm64/android-ndk-$(ANDROID_NDK_VERSION))
	cd cmd/streampanel && \
		PKG_CONFIG_LIBS_FORCE_STATIC='libav*,libvlc' \
		PKG_CONFIG_ERASE="-fopenmp=*,-landroid" \
		PKG_CONFIG='$(GOPATH)/bin/pkg-config-wrapper' \
		PKG_CONFIG_PATH='$(PWD)/3rdparty/arm64/termux/data/data/com.termux/files/usr/lib/pkgconfig' \
		CGO_CFLAGS='-I$(ANDROID_NDK_HOME)/toolchains/llvm/prebuilt/linux-x86_64/sysroot/usr/include/ -I$(PWD)/3rdparty/arm64/termux/data/data/com.termux/files/usr/include -Wno-incompatible-function-pointer-types -Wno-unused-result -Wno-xor-used-as-pow' \
		CGO_LDFLAGS='-ldl -lc -L$(ANDROID_NDK_HOME)/toolchains/llvm/prebuilt/linux-x86_64/sysroot/usr/lib/ -L$(PWD)/3rdparty/arm64/termux/data/data/com.termux/files/usr/lib' \
		ANDROID_NDK_HOME="$(ANDROID_NDK_HOME)" \
		fyne package $(FYNEBUILD_FLAGS) -release -os android/arm64 && mv streampanel.apk ../../build/streampanel-arm64.apk

check-streampanel-android-arm64-static-cgo:
	$(eval TEMP_DIR:=$(shell mktemp -d))
	@echo "temp_dir:<$(TEMP_DIR)>"
	@cp build/streampanel-arm64.apk "$(TEMP_DIR)/streampanel.zip"
	@cd "$(TEMP_DIR)" && unzip streampanel.zip >/dev/null
	@if readelf -d "$(TEMP_DIR)/lib/arm64-v8a/libstreampanel.so" | grep STATIC_TLS; then \
		readelf -d "$(TEMP_DIR)/lib/arm64-v8a/libstreampanel.so"; \
		echo "The resulting APK is linked in a wrong way, 'readlink -d' showed flag 'STATIC_TLS', so the application will crash when you will try to launch it."; \
		rm -rf "$(TEMP_DIR)"; \
		exit 1; \
	fi
	@if ! readelf -d "$(TEMP_DIR)/lib/arm64-v8a/libstreampanel.so" | grep libdl.so >/dev/null; then \
		readelf -d "$(TEMP_DIR)/lib/arm64-v8a/libstreampanel.so"; \
		echo "The resulting APK is linked in a wrong way, 'readlink -d' showed it does not use 'libdl.so', which likely means some wacky stuff happened and the application is not guaranteed to work."; \
		rm -rf "$(TEMP_DIR)"; \
		exit 1; \
	fi
	rm -rf "$(TEMP_DIR)"

streampanel-android-arm64: checkconfig-android builddir 3rdparty/arm64/android-ndk-$(ANDROID_NDK_VERSION)
	$(eval ANDROID_NDK_HOME=$(PWD)/3rdparty/arm64/android-ndk-$(ANDROID_NDK_VERSION))
	cd cmd/streampanel && ANDROID_NDK_HOME="$(ANDROID_NDK_HOME)" fyne package $(FYNEBUILD_FLAGS) -release -os android/arm64 && mv streampanel-arm64.apk ../../build/

install-android-arm64:
	adb shell pm uninstall center.dx.streampanel >/dev/null 2>&1 || /bin/true
	adb install build/streampanel-arm64.apk

streampanel-ios: builddir
	cd cmd/streampanel && fyne package $(GOBUILD_FLAGS) -release -os ios && mv streampanel.ipa ../../build/

streampanel-windows: windows-builddir windows-deps
	PKG_CONFIG_PATH=$(WINDOWS_PKG_CONFIG_PATH) CGO_ENABLED=1 CGO_LDFLAGS="-static" CGO_CFLAGS="$(WINDOWS_CGO_FLAGS)" CC=x86_64-w64-mingw32-gcc GOOS=windows go build $(GOBUILD_FLAGS) -ldflags "-H windowsgui $(LINKER_FLAGS_WINDOWS)" -o build/streampanel-windows-amd64/streampanel.exe ./cmd/streampanel/

streampanel-windows-debug: windows-builddir windows-debug-deps
	PKG_CONFIG_PATH=$(WINDOWS_PKG_CONFIG_PATH) CGO_ENABLED=1 CGO_LDFLAGS="-static" CGO_CFLAGS="$(WINDOWS_CGO_FLAGS)" CC=x86_64-w64-mingw32-gcc GOOS=windows go build $(GOBUILD_FLAGS) -ldflags "-a $(LINKER_FLAGS_WINDOWS)" -o build/streampanel-windows-debug-amd64/streampanel-debug.exe ./cmd/streampanel/

streamd-linux-amd64: builddir
	CGO_ENABLED=1 CGO_LDFLAGS="-static" GOOS=linux GOARCH=amd64 go build -o build/streamd-linux-amd64 ./cmd/streamd

streamcli-linux-amd64: builddir
	CGO_ENABLED=0 CGO_LDFLAGS="-static" GOOS=linux GOARCH=amd64 go build -o build/streamcli-linux-amd64 ./cmd/streamcli

streamcli-linux-arm64: builddir
	CGO_ENABLED=0 CGO_LDFLAGS="-static" GOOS=linux GOARCH=arm64 go build -o build/streamcli-linux-arm64 ./cmd/streamcli

player-windows: windows-builddir windows-deps
	PKG_CONFIG_PATH=$(WINDOWS_PKG_CONFIG_PATH) CGO_ENABLED=1 CGO_LDFLAGS="-static" CGO_CFLAGS="$(WINDOWS_CGO_FLAGS)" CC=x86_64-w64-mingw32-gcc GOOS=windows go build $(GOBUILD_FLAGS) -ldflags "$(LINKER_FLAGS_WINDOWS)" -o build/streampanel-windows-amd64/player.exe ./pkg/player/cmd/player/

streamplayer-windows: windows-builddir windows-deps
	PKG_CONFIG_PATH=$(WINDOWS_PKG_CONFIG_PATH) CGO_ENABLED=1 CGO_LDFLAGS="-static" CGO_CFLAGS="$(WINDOWS_CGO_FLAGS)" CC=x86_64-w64-mingw32-gcc GOOS=windows go build $(GOBUILD_FLAGS) -ldflags "$(LINKER_FLAGS_WINDOWS)" -o build/streampanel-windows-amd64/streamplayer.exe ./pkg/streamplayer/cmd/streamplayer/

builddir:
	mkdir -p build

streampanel-windows-amd64.zip: streampanel-windows
	sh -c 'cd build && zip -r streampanel-windows-amd64.zip streampanel-windows-amd64'

streampanel-windows-debug-amd64.zip: streampanel-windows-debug
	sh -c 'cd build && zip -r streampanel-windows-debug-amd64.zip streampanel-windows-debug-amd64'
