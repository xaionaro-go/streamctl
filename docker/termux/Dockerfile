FROM ghcr.io/termux/package-builder:latest

RUN sudo apt update && sudo apt dist-upgrade -fy && sudo apt clean

USER builder
WORKDIR /home/builder
RUN git clone https://github.com/xaionaro/termux-packages
WORKDIR /home/builder/termux-packages

RUN ./scripts/setup-android-sdk.sh

# downloading dependencies (e.g. we do not need ccls,
# but we need the most of the dependencies of ccls)
RUN ./build-package.sh -I gettext    || /bin/true
RUN ./build-package.sh -I ccls       || /bin/true
RUN ./build-package.sh -I termux-api || /bin/true
RUN ./build-package.sh -I xdotool    || /bin/true
RUN ./build-package.sh -I xdg-utils  || /bin/true

# building what we need	
RUN rm -f /data/data/.built-packages/liblzma  && ./build-package.sh -I liblzma
RUN rm -f /data/data/.built-packages/libiconv && ./build-package.sh -I libiconv
RUN ./build-package.sh -I libx11
RUN ./build-package.sh ffmpeg	
RUN ./build-package.sh libxxf86vm	
#RUN ./build-package.sh vlc

# installing fyne
RUN sudo apt install -y golang-go
RUN go install fyne.io/fyne/v2/cmd/fyne@latest

# avoiding fyne loading wrong GL libraries:
RUN rm -f /data/data/com.termux/files/usr/lib/*lib*GL*