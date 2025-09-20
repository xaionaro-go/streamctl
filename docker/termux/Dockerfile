FROM ghcr.io/termux/package-builder:latest

RUN sudo apt update && sudo apt dist-upgrade -fy && sudo apt install -y ccache && sudo apt clean

USER builder
ENV CCACHE_DIR=/home/builder/.cache/ccache
ENV CCACHE_MAXSIZE=20G
ENV CCACHE_COMPRESS=1
ENV PATH="/usr/lib/ccache:${PATH}"
ENV SCCACHE_DIR=/home/builder/.cache/sccache
ENV SCCACHE_CACHE_SIZE=20G
ENV RUSTC_WRAPPER=/usr/bin/sccache
ENV CARGO_HOME=/home/builder/.cargo
ENV CARGO_TARGET_DIR=/home/builder/.cargo-target

WORKDIR /home/builder
RUN git clone https://github.com/xaionaro/termux-packages
WORKDIR /home/builder/termux-packages

RUN ./scripts/setup-android-sdk.sh

# downloading dependencies (e.g. we do not need ccls,
# but we need the most of the dependencies of ccls)
RUN ./build-package.sh -I -d gettext    || /bin/true
RUN ./build-package.sh -I -d ccls       || /bin/true
RUN ./build-package.sh -I -d termux-api || /bin/true
RUN ./build-package.sh -I -d xdotool    || /bin/true
RUN ./build-package.sh -I -d xdg-utils  || /bin/true

# checkout the version we need
RUN git fetch && git checkout b2d245ec19e5d5d8a0b60f1d31decaad7b1beca7

# building what we need	
RUN rm -f /data/data/.built-packages/liblzma                    && ./build-package.sh -d -I liblzma
RUN rm -f /data/data/.built-packages/libiconv                   && ./build-package.sh -d -I libiconv
RUN rm -f /data/data/.built-packages/libx11                     && ./build-package.sh -d -I libx11
RUN rm -f /data/data/.built-packages/libxxf86vm                 && ./build-package.sh -d libxxf86vm	
RUN rm -f /data/data/.built-packages/openssl                    && ./build-package.sh -d openssl
RUN rm -f /data/data/.built-packages/libandroid-posix-semaphore && ./build-package.sh -d -I libandroid-posix-semaphore

# finicky dependencies of ffmpeg
RUN sed -e 's/make -f Makefile$/make -f Makefile-libbz2_so/' -i ./packages/libbz2/build.sh

# build custom ffmpeg
COPY ffmpeg_mediacodec_set_parameters.patch /home/builder/termux-packages/packages/ffmpeg/mediacodec_set_parameters.patch
COPY ffmpeg_mediacodec_q_params.patch /home/builder/termux-packages/packages/ffmpeg/mediacodec_q_params.patch
RUN ./build-package.sh -d ffmpeg

# installing fyne
RUN sudo apt install -y golang-go
RUN go install fyne.io/fyne/v2/cmd/fyne@latest

# avoiding fyne loading wrong GL libraries:
RUN rm -f /data/data/com.termux/files/usr/lib/*lib*GL*
