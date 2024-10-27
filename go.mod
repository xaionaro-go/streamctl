module github.com/xaionaro-go/streamctl

go 1.23

toolchain go1.23.2

// The original go-yaml is very slow, using the improved version instead
replace github.com/goccy/go-yaml v1.11.3 => github.com/yoelsusanto/go-yaml v0.0.0-20240324162521-2018c1ab915b

replace github.com/andreykaipov/goobs v1.4.1 => github.com/xaionaro-go/goobs v0.0.0-20241025144519-45ebde014c09

replace github.com/adrg/libvlc-go/v3 v3.1.5 => github.com/xaionaro-go/libvlc-go/v3 v3.0.0-20241011194409-0fe4e2a9d901

replace fyne.io/fyne/v2 v2.5.0 => github.com/xaionaro-go/fyne/v2 v2.0.0-20241020235352-fd61e4920f24

replace code.cloudfoundry.org/bytefmt => github.com/cloudfoundry/bytefmt v0.0.0-20211005130812-5bb3c17173e5

replace github.com/pion/ice/v2 => github.com/aler9/ice/v2 v2.0.0-20241006110309-c973995af023

replace github.com/pion/webrtc/v3 => github.com/aler9/webrtc/v3 v3.0.0-20240610104456-eaec24056d06

replace github.com/jfreymuth/pulse v0.1.1 => github.com/xaionaro-go/pulse v0.0.0-20241023202712-7151fa00d4bb

require (
	github.com/facebookincubator/go-belt v0.0.0-20240804203001-846c4409d41c
	github.com/go-git/go-billy/v5 v5.5.0
	github.com/goccy/go-yaml v1.11.3
	github.com/hashicorp/go-multierror v1.1.1
	github.com/nicklaw5/helix/v2 v2.30.1-0.20240715193454-0151ccccf980
	github.com/spf13/cobra v1.8.0
	github.com/xaionaro-go/logrustash v0.0.0-20240804141650-d48034780a5f
	golang.org/x/oauth2 v0.22.0
	google.golang.org/api v0.192.0
)

require (
	cloud.google.com/go/auth v0.8.1 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.3 // indirect
	cloud.google.com/go/compute/metadata v0.5.0 // indirect
	code.cloudfoundry.org/bytefmt v0.0.0 // indirect
	dario.cat/mergo v1.0.0 // indirect
	fyne.io/systray v1.11.0 // indirect
	github.com/BurntSushi/toml v1.4.0 // indirect
	github.com/Danny-Dasilva/CycleTLS/cycletls v0.0.0-20220620102923-c84d740b4757 // indirect
	github.com/Danny-Dasilva/fhttp v0.0.0-20220524230104-f801520157d6 // indirect
	github.com/Danny-Dasilva/utls v0.0.0-20220604023528-30cb107b834e // indirect
	github.com/MicahParks/jwkset v0.5.20 // indirect
	github.com/MicahParks/keyfunc/v3 v3.3.5 // indirect
	github.com/Microsoft/go-winio v0.6.1 // indirect
	github.com/ProtonMail/go-crypto v1.0.0 // indirect
	github.com/RomainMichau/cloudscraper_go v0.4.1 // indirect
	github.com/abema/go-mp4 v1.2.0 // indirect
	github.com/andybalholm/brotli v1.1.0 // indirect
	github.com/asticode/go-astits v1.13.0 // indirect
	github.com/benburkert/openpgp v0.0.0-20160410205803-c2471f86866c // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/bluenviron/gohlslib/v2 v2.0.0 // indirect
	github.com/bluenviron/mediacommon v1.13.0 // indirect
	github.com/buger/jsonparser v1.1.1 // indirect
	github.com/bytedance/sonic v1.11.6 // indirect
	github.com/bytedance/sonic/loader v0.1.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cloudflare/circl v1.3.7 // indirect
	github.com/cloudwego/base64x v0.1.4 // indirect
	github.com/cloudwego/iasm v0.2.0 // indirect
	github.com/cyphar/filepath-securejoin v0.2.4 // indirect
	github.com/datarhei/gosrt v0.7.0 // indirect
	github.com/dsnet/compress v0.0.1 // indirect
	github.com/ebitengine/purego v0.8.0 // indirect
	github.com/emirpasic/gods v1.18.1 // indirect
	github.com/fatih/color v1.15.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fredbi/uri v1.1.0 // indirect
	github.com/fsnotify/fsnotify v1.7.0 // indirect
	github.com/fyne-io/gl-js v0.0.0-20230506162202-1fdaa286a934 // indirect
	github.com/fyne-io/glfw-js v0.0.0-20240101223322-6e1efdc71b7a // indirect
	github.com/fyne-io/image v0.0.0-20240417123036-dc0ee9e7c964 // indirect
	github.com/gabriel-vasile/mimetype v1.4.3 // indirect
	github.com/gen2brain/shm v0.0.0-20230802011745-f2460f5984f7 // indirect
	github.com/gin-contrib/sse v0.1.0 // indirect
	github.com/gin-gonic/gin v1.10.0 // indirect
	github.com/go-git/gcfg v1.5.1-0.20230307220236-3a3c6141e376 // indirect
	github.com/go-gl/gl v0.0.0-20231021071112-07e5d0ea2e71 // indirect
	github.com/go-gl/glfw/v3.3/glfw v0.0.0-20240506104042-037f3cc74f2a // indirect
	github.com/go-logr/logr v1.4.2 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ng/slices v0.0.0-20230703171042-6195d35636a2 // indirect
	github.com/go-ng/sort v0.0.0-20220617173827-2cc7cd04f7c7 // indirect
	github.com/go-ng/xatomic v0.0.0-20230519181013-85c0ec87e55f // indirect
	github.com/go-ng/xsort v0.0.0-20220617174223-1d146907bccc // indirect
	github.com/go-ole/go-ole v1.2.6 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.20.0 // indirect
	github.com/go-text/render v0.1.1-0.20240418202334-dd62631dae9b // indirect
	github.com/go-text/typesetting v0.1.1 // indirect
	github.com/goccy/go-json v0.10.2 // indirect
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/golang-jwt/jwt/v4 v4.0.0 // indirect
	github.com/golang-jwt/jwt/v5 v5.2.1 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/google/go-querystring v1.1.0 // indirect
	github.com/google/pprof v0.0.0-20240207164012-fb44976bdcd5 // indirect
	github.com/google/s2a-go v0.1.8 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.2 // indirect
	github.com/googleapis/gax-go/v2 v2.13.0 // indirect
	github.com/gookit/color v1.5.4 // indirect
	github.com/gopherjs/gopherjs v1.17.2 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/logutils v1.0.0 // indirect
	github.com/huandu/go-tls v0.0.0-20200109070953-6f75fb441850 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jbenet/go-context v0.0.0-20150711004518-d14ea06fba99 // indirect
	github.com/jeandeaual/go-locale v0.0.0-20240223122105-ce5225dcaa49 // indirect
	github.com/jezek/xgb v1.1.0 // indirect
	github.com/jfreymuth/vorbis v1.0.2 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/jsummers/gobmp v0.0.0-20230614200233-a9de23ed2e25 // indirect
	github.com/kballard/go-shellquote v0.0.0-20180428030007-95032a82bc51 // indirect
	github.com/kevinburke/ssh_config v1.2.0 // indirect
	github.com/klauspost/cpuid/v2 v2.2.7 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/lxn/win v0.0.0-20210218163916-a377121e959e // indirect
	github.com/matthewhartstonge/argon2 v1.0.1 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/mmcloughlin/profile v0.1.1 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/nicksnyder/go-i18n/v2 v2.4.0 // indirect
	github.com/nu7hatch/gouuid v0.0.0-20131221200532-179d4d0c4d8d // indirect
	github.com/pelletier/go-toml/v2 v2.2.2 // indirect
	github.com/pion/ice/v2 v2.3.24 // indirect
	github.com/pion/interceptor v0.1.37 // indirect
	github.com/pion/logging v0.2.2 // indirect
	github.com/pion/rtcp v1.2.14 // indirect
	github.com/pion/rtp v1.8.9 // indirect
	github.com/pion/sdp/v3 v3.0.9 // indirect
	github.com/pjbgf/sha1cd v0.3.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/client_model v0.6.0 // indirect
	github.com/prometheus/common v0.47.0 // indirect
	github.com/prometheus/procfs v0.12.0 // indirect
	github.com/rymdport/portal v0.2.6 // indirect
	github.com/sergi/go-diff v1.3.2-0.20230802210424-5b0b94c5c0d3 // indirect
	github.com/skeema/knownhosts v1.2.2 // indirect
	github.com/srwiley/oksvg v0.0.0-20221011165216-be6e8873101c // indirect
	github.com/srwiley/rasterx v0.0.0-20220730225603-2ab79fcdd4ef // indirect
	github.com/tklauser/go-sysconf v0.3.14 // indirect
	github.com/tklauser/numcpus v0.8.0 // indirect
	github.com/twitchyliquid64/golang-asm v0.15.1 // indirect
	github.com/ugorji/go/codec v1.2.12 // indirect
	github.com/xaionaro-go/spinlock v0.0.0-20200518175509-30e6d1ce68a1 // indirect
	github.com/xanzy/ssh-agent v0.3.3 // indirect
	github.com/xo/terminfo v0.0.0-20210125001918-ca9a967f8778 // indirect
	github.com/yuin/goldmark v1.7.1 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	github.com/yutopp/go-amf0 v0.1.0 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.49.0 // indirect
	go.opentelemetry.io/otel v1.24.0 // indirect
	go.opentelemetry.io/otel/metric v1.24.0 // indirect
	go.opentelemetry.io/otel/trace v1.24.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
	golang.org/x/arch v0.8.0 // indirect
	golang.org/x/exp v0.0.0-20240213143201-ec583247a57a // indirect
	golang.org/x/image v0.18.0 // indirect
	golang.org/x/mobile v0.0.0-20240404231514-09dbf07665ed // indirect
	golang.org/x/mod v0.18.0 // indirect
	golang.org/x/sync v0.8.0 // indirect
	golang.org/x/sys v0.26.0 // indirect
	golang.org/x/term v0.25.0 // indirect
	golang.org/x/text v0.19.0 // indirect
	golang.org/x/time v0.6.0 // indirect
	golang.org/x/tools v0.22.0 // indirect
	golang.org/x/xerrors v0.0.0-20220907171357-04be3eba64a2 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240730163845-b1a4ccb954bf // indirect
	gopkg.in/natefinch/npipe.v2 v2.0.0-20160621034901-c1b8fa8bdcce // indirect
	gopkg.in/warnings.v0 v0.1.2 // indirect
	lukechampine.com/blake3 v1.2.1 // indirect
)

require (
	fyne.io/fyne/v2 v2.5.0
	github.com/AgustinSRG/go-child-process-manager v1.0.1
	github.com/BurntSushi/xgb v0.0.0-20160522181843-27f122750802
	github.com/DataDog/gostackparse v0.6.0
	github.com/DexterLB/mpvipc v0.0.0-20230829142118-145d6eabdc37
	github.com/abhinavxd/youtube-live-chat-downloader/v2 v2.0.3
	github.com/adeithe/go-twitch v0.3.1
	github.com/adrg/libvlc-go/v3 v3.1.5
	github.com/andreykaipov/goobs v1.4.1
	github.com/anthonynsimon/bild v0.14.0
	github.com/asaskevich/EventBus v0.0.0-20200907212545-49d423059eef
	github.com/asticode/go-astiav v0.19.0
	github.com/asticode/go-astikit v0.42.0
	github.com/bamiaux/rez v0.0.0-20170731184118-29f4463c688b
	github.com/blang/mpv v0.0.0-20160810175505-d56d7352e068
	github.com/bluenviron/gortsplib/v4 v4.11.0
	github.com/chai2010/webp v1.1.1
	github.com/davecgh/go-spew v1.1.1
	github.com/dustin/go-humanize v1.0.1
	github.com/ebitengine/oto/v3 v3.3.1
	github.com/getsentry/sentry-go v0.28.1
	github.com/go-git/go-git/v5 v5.12.0
	github.com/go-ng/xmath v0.0.0-20230704233441-028f5ea62335
	github.com/go-yaml/yaml v2.1.0+incompatible
	github.com/google/go-github/v66 v66.0.0
	github.com/google/uuid v1.6.0
	github.com/grpc-ecosystem/go-grpc-middleware v1.4.0
	github.com/iancoleman/strcase v0.3.0
	github.com/immune-gmbh/attestation-sdk v0.0.0-20230711173209-f44e4502aeca
	github.com/jfreymuth/oggvorbis v1.0.5
	github.com/jfreymuth/pulse v0.1.1
	github.com/kbinani/screenshot v0.0.0-20230812210009-b87d31814237
	github.com/lusingander/colorpicker v0.7.3
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.18.0
	github.com/sethvargo/go-password v0.3.1
	github.com/shirou/gopsutil v3.21.11+incompatible
	github.com/sirupsen/logrus v1.9.3
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.9.0
	github.com/xaionaro-go/datacounter v1.0.4
	github.com/xaionaro-go/go-rtmp v0.0.0-20241009130244-1e3160f27f42
	github.com/xaionaro-go/gorex v0.0.0-20241010205749-bcd59d639c4d
	github.com/xaionaro-go/kickcom v0.0.0-20241022142825-25a234cc8628
	github.com/xaionaro-go/lockmap v0.0.0-20240901172806-e17aea364748
	github.com/xaionaro-go/mediamtx v0.0.0-20241009124606-94c22c603970
	github.com/xaionaro-go/object v0.0.0-20241024025057-382352276e7b
	github.com/xaionaro-go/obs-grpc-proxy v0.0.0-20241018162120-5faf4e7a684a
	github.com/xaionaro-go/timeapiio v0.0.0-20240915203246-b907cf699af3
	github.com/xaionaro-go/typing v0.0.0-20221123235249-2229101d38ba
	github.com/xaionaro-go/unsafetools v0.0.0-20241024014258-a46e1ce3763e
	github.com/yutopp/go-flv v0.3.1
	golang.org/x/crypto v0.28.0
	golang.org/x/net v0.30.0
	google.golang.org/grpc v1.65.0
	google.golang.org/protobuf v1.34.2
	gopkg.in/yaml.v2 v2.4.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/BurntSushi/xgbutil v0.0.0-20190907113008-ad855c713046
	github.com/onsi/gomega v1.30.0 // indirect
	github.com/phuslu/goid v1.0.1 // indirect
	github.com/pion/datachannel v1.5.6 // indirect
	github.com/pion/dtls/v2 v2.2.11 // indirect
	github.com/pion/mdns v0.0.12 // indirect
	github.com/pion/randutil v0.1.0 // indirect
	github.com/pion/sctp v1.8.16 // indirect
	github.com/pion/srtp/v2 v2.0.18 // indirect
	github.com/pion/stun v0.6.1 // indirect
	github.com/pion/transport/v2 v2.2.5 // indirect
	github.com/pion/turn/v2 v2.1.6 // indirect
	github.com/pion/webrtc/v3 v3.2.23 // indirect
)
