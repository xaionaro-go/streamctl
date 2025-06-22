module github.com/xaionaro-go/streamctl

go 1.24.1

// The original go-yaml is very slow, using the improved version instead
replace github.com/goccy/go-yaml v1.11.3 => github.com/yoelsusanto/go-yaml v0.0.0-20240324162521-2018c1ab915b

replace github.com/andreykaipov/goobs v1.4.1 => github.com/xaionaro-go/goobs v0.0.0-20241103210141-030e538ac440

replace github.com/adrg/libvlc-go/v3 v3.1.5 => github.com/xaionaro-go/libvlc-go/v3 v3.0.0-20241011194409-0fe4e2a9d901

replace fyne.io/fyne/v2 v2.5.4 => github.com/xaionaro-go/fyne/v2 v2.0.0-20250215180758-399edb421067

replace code.cloudfoundry.org/bytefmt => github.com/cloudfoundry/bytefmt v0.0.0-20211005130812-5bb3c17173e5

replace github.com/jfreymuth/pulse v0.1.1 => github.com/xaionaro-go/pulse v0.0.0-20241023202712-7151fa00d4bb

replace github.com/rs/zerolog v1.33.0 => github.com/xaionaro-go/zerolog2belt v0.0.0-20241103164018-a3bc1ea487e5

replace github.com/bluenviron/gortsplib/v4 v4.11.0 => github.com/xaionaro-go/gortsplib/v4 v4.0.0-20241123213409-7279dabb7de6

replace github.com/wlynxg/anet => github.com/BieHDC/anet v0.0.6-0.20241226223613-d47f8b766b3c

replace github.com/nicklaw5/helix/v2 v2.30.1-0.20240715193454-0151ccccf980 => github.com/xaionaro-go/helix/v2 v2.0.0-20250309182928-f54c9d4c8a29

replace github.com/asticode/go-astiav v0.35.1 => github.com/xaionaro-go/astiav v0.0.0-20250406220418-87d14d2908f9

replace github.com/bluenviron/mediacommon/v2 v2.0.1-0.20250324151931-b8ce69d15d3d => github.com/xaionaro-go/mediacommon/v2 v2.0.0-20250420012906-03d6d69ac3b7

require (
	github.com/facebookincubator/go-belt v0.0.0-20250308011339-62fb7027b11f
	github.com/go-git/go-billy/v5 v5.6.2
	github.com/goccy/go-yaml v1.17.1
	github.com/hashicorp/go-multierror v1.1.1
	github.com/nicklaw5/helix/v2 v2.30.1-0.20240715193454-0151ccccf980
	github.com/spf13/cobra v1.8.1
	github.com/xaionaro-go/logrustash v0.0.0-20240804141650-d48034780a5f // indirect
	golang.org/x/oauth2 v0.28.0
	google.golang.org/api v0.203.0
)

require (
	cloud.google.com/go/auth v0.9.9 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.4 // indirect
	cloud.google.com/go/compute/metadata v0.6.0 // indirect
	code.cloudfoundry.org/bytefmt v0.34.0 // indirect
	dario.cat/mergo v1.0.0 // indirect
	fyne.io/systray v1.11.0 // indirect
	github.com/BurntSushi/toml v1.5.0 // indirect
	github.com/Danny-Dasilva/CycleTLS/cycletls v1.0.26 // indirect
	github.com/DataDog/gostackparse v0.7.0 // indirect
	github.com/MicahParks/jwkset v0.8.0 // indirect
	github.com/MicahParks/keyfunc/v3 v3.3.10 // indirect
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/ProtonMail/go-crypto v1.1.5 // indirect
	github.com/RomainMichau/cloudscraper_go v0.4.1 // indirect
	github.com/abema/go-mp4 v1.4.1 // indirect
	github.com/adrg/libvlc-go/v3 v3.1.6 // indirect
	github.com/andybalholm/brotli v1.1.0 // indirect
	github.com/asticode/go-astikit v0.54.0 // indirect
	github.com/asticode/go-astits v1.13.0 // indirect
	github.com/benburkert/openpgp v0.0.0-20160410205803-c2471f86866c // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/mpv v0.0.0-20160810175505-d56d7352e068 // indirect
	github.com/bluenviron/gohlslib/v2 v2.1.4-0.20250210133907-d3dddacbb9fc // indirect
	github.com/bluenviron/mediacommon/v2 v2.0.1-0.20250324151931-b8ce69d15d3d // indirect
	github.com/buger/jsonparser v1.1.1 // indirect
	github.com/bytedance/sonic v1.13.3 // indirect
	github.com/bytedance/sonic/loader v0.2.4 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cloudflare/circl v1.6.0 // indirect
	github.com/cloudwego/base64x v0.1.5 // indirect
	github.com/cloudwego/eino-ext/libs/acl/openai v0.0.0-20250422092704-54e372e1fa3d // indirect
	github.com/coreos/go-oidc/v3 v3.11.0 // indirect
	github.com/cyphar/filepath-securejoin v0.4.1 // indirect
	github.com/datarhei/gosrt v0.9.0 // indirect
	github.com/dexterlb/mpvipc v0.0.0-20241005113212-7cdefca0e933 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/ebitengine/oto/v3 v3.3.2 // indirect
	github.com/ebitengine/purego v0.8.0 // indirect
	github.com/emirpasic/gods v1.18.1 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fredbi/uri v1.1.0 // indirect
	github.com/friendsofgo/errors v0.9.2 // indirect
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/fyne-io/gl-js v0.1.0 // indirect
	github.com/fyne-io/glfw-js v0.2.0 // indirect
	github.com/fyne-io/image v0.1.1 // indirect
	github.com/fyne-io/oksvg v0.1.0 // indirect
	github.com/gabriel-vasile/mimetype v1.4.7 // indirect
	github.com/gen2brain/shm v0.0.0-20230802011745-f2460f5984f7 // indirect
	github.com/getkin/kin-openapi v0.118.0 // indirect
	github.com/gin-contrib/sse v0.1.0 // indirect
	github.com/gin-gonic/gin v1.10.0 // indirect
	github.com/go-git/gcfg v1.5.1-0.20230307220236-3a3c6141e376 // indirect
	github.com/go-gl/gl v0.0.0-20231021071112-07e5d0ea2e71 // indirect
	github.com/go-gl/glfw/v3.3/glfw v0.0.0-20240506104042-037f3cc74f2a // indirect
	github.com/go-gorp/gorp/v3 v3.1.0 // indirect
	github.com/go-jose/go-jose/v4 v4.0.5 // indirect
	github.com/go-logr/logr v1.4.2 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ng/container v0.0.0-20220615121757-4740bf4bbc52 // indirect
	github.com/go-ng/slices v0.0.0-20230703171042-6195d35636a2 // indirect
	github.com/go-ng/sort v0.0.0-20220617173827-2cc7cd04f7c7 // indirect
	github.com/go-ng/xsort v0.0.0-20250330112557-d2ee7f01661c // indirect
	github.com/go-ole/go-ole v1.2.6 // indirect
	github.com/go-openapi/jsonpointer v0.19.5 // indirect
	github.com/go-openapi/swag v0.19.5 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.23.0 // indirect
	github.com/go-redis/redis/v8 v8.11.5 // indirect
	github.com/go-text/render v0.2.0 // indirect
	github.com/go-text/typesetting v0.2.1 // indirect
	github.com/goccy/go-json v0.10.4 // indirect
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/gofrs/uuid v4.4.0+incompatible // indirect
	github.com/golang-jwt/jwt/v4 v4.5.1 // indirect
	github.com/golang-jwt/jwt/v5 v5.2.2 // indirect
	github.com/golang/groupcache v0.0.0-20241129210726-2c02b8208cf8 // indirect
	github.com/google/go-querystring v1.1.0 // indirect
	github.com/google/gopacket v1.1.19 // indirect
	github.com/google/pprof v0.0.0-20240207164012-fb44976bdcd5 // indirect
	github.com/google/s2a-go v0.1.8 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.4 // indirect
	github.com/googleapis/gax-go/v2 v2.13.0 // indirect
	github.com/gookit/color v1.5.4 // indirect
	github.com/goph/emperror v0.17.2 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/hack-pad/go-indexeddb v0.3.2 // indirect
	github.com/hack-pad/safejs v0.1.0 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/logutils v1.0.0 // indirect
	github.com/huandu/go-tls v1.0.1 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/invopop/yaml v0.1.0 // indirect
	github.com/jbenet/go-context v0.0.0-20150711004518-d14ea06fba99 // indirect
	github.com/jeandeaual/go-locale v0.0.0-20241217141322-fcc2cadd6f08 // indirect
	github.com/jezek/xgb v1.1.0 // indirect
	github.com/jfreymuth/oggvorbis v1.0.5 // indirect
	github.com/jfreymuth/pulse v0.1.1 // indirect
	github.com/jfreymuth/vorbis v1.0.2 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/jsummers/gobmp v0.0.0-20230614200233-a9de23ed2e25 // indirect
	github.com/kballard/go-shellquote v0.0.0-20180428030007-95032a82bc51 // indirect
	github.com/kevinburke/ssh_config v1.2.0 // indirect
	github.com/klauspost/cpuid/v2 v2.2.10 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/lib/pq v1.10.9 // indirect
	github.com/lxn/win v0.0.0-20210218163916-a377121e959e // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/matthewhartstonge/argon2 v1.2.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/meguminnnnnnnnn/go-openai v0.0.0-20250408071642-761325becfd6 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/mmcloughlin/profile v0.1.1 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/mohae/deepcopy v0.0.0-20170929034955-c48cc78d4826 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/nfnt/resize v0.0.0-20180221191011-83c6a9932646 // indirect
	github.com/nicksnyder/go-i18n/v2 v2.5.1 // indirect
	github.com/nikolalohinski/gonja v1.5.3 // indirect
	github.com/nu7hatch/gouuid v0.0.0-20131221200532-179d4d0c4d8d // indirect
	github.com/pelletier/go-toml/v2 v2.2.3 // indirect
	github.com/perimeterx/marshmallow v1.1.4 // indirect
	github.com/pion/dtls/v3 v3.0.4 // indirect
	github.com/pion/ice/v2 v2.3.34 // indirect
	github.com/pion/ice/v4 v4.0.7 // indirect
	github.com/pion/interceptor v0.1.37 // indirect
	github.com/pion/logging v0.2.3 // indirect
	github.com/pion/mdns/v2 v2.0.7 // indirect
	github.com/pion/rtcp v1.2.15 // indirect
	github.com/pion/rtp v1.8.13 // indirect
	github.com/pion/sdp/v3 v3.0.11 // indirect
	github.com/pion/srtp/v3 v3.0.4 // indirect
	github.com/pion/stun/v3 v3.0.0 // indirect
	github.com/pion/transport/v3 v3.0.7 // indirect
	github.com/pion/turn/v4 v4.0.0 // indirect
	github.com/pion/webrtc/v4 v4.0.7 // indirect
	github.com/pjbgf/sha1cd v0.3.2 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/pojntfx/go-auth-utils v0.1.0 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.62.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	github.com/quic-go/quic-go v0.41.0 // indirect
	github.com/refraction-networking/utls v1.6.2 // indirect
	github.com/rubenv/sql-migrate v1.7.0 // indirect
	github.com/rymdport/portal v0.4.1 // indirect
	github.com/sergi/go-diff v1.3.2-0.20230802210424-5b0b94c5c0d3 // indirect
	github.com/skeema/knownhosts v1.3.1 // indirect
	github.com/slongfield/pyfmt v0.0.0-20220222012616-ea85ff4c361f // indirect
	github.com/songgao/water v0.0.0-20200317203138-2b4b6d7c09d8 // indirect
	github.com/spf13/cast v1.7.1 // indirect
	github.com/srwiley/oksvg v0.0.0-20221011165216-be6e8873101c // indirect
	github.com/srwiley/rasterx v0.0.0-20220730225603-2ab79fcdd4ef // indirect
	github.com/teivah/broadcast v0.1.0 // indirect
	github.com/tklauser/go-sysconf v0.3.14 // indirect
	github.com/tklauser/numcpus v0.8.0 // indirect
	github.com/twitchyliquid64/golang-asm v0.15.1 // indirect
	github.com/ugorji/go/codec v1.2.12 // indirect
	github.com/vishvananda/netlink v1.3.0 // indirect
	github.com/vishvananda/netns v0.0.4 // indirect
	github.com/volatiletech/inflect v0.0.1 // indirect
	github.com/volatiletech/null/v8 v8.1.2 // indirect
	github.com/volatiletech/randomize v0.0.1 // indirect
	github.com/volatiletech/sqlboiler/v4 v4.16.2 // indirect
	github.com/volatiletech/strmangle v0.0.6 // indirect
	github.com/wlynxg/anet v0.0.6-0.20250109065809-5501d401a269 // indirect
	github.com/xaionaro-go/avmediacodec v0.0.0-20250421150856-ddd390422c21 // indirect
	github.com/xaionaro-go/gorex v0.0.0-20241010205749-bcd59d639c4d // indirect
	github.com/xaionaro-go/libsrt v0.0.0-20250105232601-e760c79b2bc3 // indirect
	github.com/xaionaro-go/ndk v0.0.0-20250420195304-361bb98583bf // indirect
	github.com/xaionaro-go/proxy v0.0.0-20250111150848-1f0e7b262638 // indirect
	github.com/xaionaro-go/spinlock v0.0.0-20200518175509-30e6d1ce68a1 // indirect
	github.com/xanzy/ssh-agent v0.3.3 // indirect
	github.com/xo/terminfo v0.0.0-20210125001918-ca9a967f8778 // indirect
	github.com/yargevad/filepathx v1.0.0 // indirect
	github.com/yuin/goldmark v1.7.8 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	github.com/yutopp/go-amf0 v0.1.1 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.opentelemetry.io/auto/sdk v1.1.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.54.0 // indirect
	go.opentelemetry.io/otel v1.34.0 // indirect
	go.opentelemetry.io/otel/metric v1.34.0 // indirect
	go.opentelemetry.io/otel/trace v1.34.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
	golang.org/x/arch v0.12.0 // indirect
	golang.org/x/exp v0.0.0-20250408133849-7e4ce0ab07d0 // indirect
	golang.org/x/image v0.24.0 // indirect
	golang.org/x/net v0.39.0 // indirect
	golang.org/x/sync v0.14.0 // indirect
	golang.org/x/sys v0.33.0 // indirect
	golang.org/x/term v0.32.0 // indirect
	golang.org/x/text v0.25.0 // indirect
	golang.org/x/time v0.9.0 // indirect
	golang.org/x/xerrors v0.0.0-20240716161551-93cc26a95ae9 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250409194420-de1ac958c67a // indirect
	gopkg.in/natefinch/npipe.v2 v2.0.0-20160621034901-c1b8fa8bdcce // indirect
	gopkg.in/warnings.v0 v0.1.2 // indirect
	h12.io/socks v1.0.3 // indirect
	lukechampine.com/blake3 v1.4.0 // indirect
)

require (
	fyne.io/fyne/v2 v2.6.1
	github.com/AgustinSRG/go-child-process-manager v1.0.1
	github.com/BurntSushi/xgb v0.0.0-20160522181843-27f122750802
	github.com/Danny-Dasilva/fhttp v0.0.0-20240217042913-eeeb0b347ce1
	github.com/abhinavxd/youtube-live-chat-downloader/v2 v2.0.3
	github.com/adeithe/go-twitch v0.3.1
	github.com/andreykaipov/goobs v1.4.1
	github.com/anthonynsimon/bild v0.14.0
	github.com/asaskevich/EventBus v0.0.0-20200907212545-49d423059eef
	github.com/asticode/go-astiav v0.35.1
	github.com/bamiaux/rez v0.0.0-20170731184118-29f4463c688b
	github.com/bluenviron/gortsplib/v4 v4.12.4-0.20250324174248-61372cfa6800
	github.com/chai2010/webp v1.1.1
	github.com/cloudwego/eino v0.3.27
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc
	github.com/dustin/go-humanize v1.0.1
	github.com/getsentry/sentry-go v0.31.1
	github.com/go-andiamo/splitter v1.2.5
	github.com/go-git/go-git/v5 v5.14.0
	github.com/go-ng/xatomic v0.0.0-20230519181013-85c0ec87e55f
	github.com/go-ng/xmath v0.0.0-20230704233441-028f5ea62335
	github.com/go-yaml/yaml v2.1.0+incompatible
	github.com/google/go-github/v66 v66.0.0
	github.com/google/uuid v1.6.0
	github.com/goombaio/namegenerator v0.0.0-20181006234301-989e774b106e
	github.com/grpc-ecosystem/go-grpc-middleware v1.4.0
	github.com/hashicorp/go-version v1.7.0
	github.com/iancoleman/strcase v0.3.0
	github.com/immune-gmbh/attestation-sdk v0.0.0-20230711173209-f44e4502aeca
	github.com/kbinani/screenshot v0.0.0-20230812210009-b87d31814237
	github.com/klauspost/compress v1.17.9
	github.com/lusingander/colorpicker v0.7.3
	github.com/pojntfx/weron v0.2.7
	github.com/prometheus/client_golang v1.20.5
	github.com/rs/zerolog v1.33.0
	github.com/scorfly/gokick v1.1.1
	github.com/sethvargo/go-password v0.3.1
	github.com/shirou/gopsutil v3.21.11+incompatible
	github.com/sirupsen/logrus v1.9.3
	github.com/skip2/go-qrcode v0.0.0-20200617195104-da1b6568686e
	github.com/spf13/pflag v1.0.6
	github.com/stretchr/testify v1.10.0
	github.com/xaionaro-go/audio v0.0.0-20250210102901-abfced9d5ef3
	github.com/xaionaro-go/avpipeline v0.0.0-20250428012319-401ac2ebe66c
	github.com/xaionaro-go/datacounter v1.0.4
	github.com/xaionaro-go/go-rtmp v0.0.0-20241009130244-1e3160f27f42
	github.com/xaionaro-go/grpcproxy v0.0.0-20241103205849-a8fef42e72f9
	github.com/xaionaro-go/iterate v0.0.0-20250406123757-7802d56b52ce
	github.com/xaionaro-go/kickcom v0.0.0-20250316223447-a1fe3d153d96
	github.com/xaionaro-go/lockmap v0.0.0-20240901172806-e17aea364748
	github.com/xaionaro-go/logwriter v0.0.0-20250111154941-c3f7a1a2d567
	github.com/xaionaro-go/mediamtx v0.0.0-20250406132618-79ecbc3e138f
	github.com/xaionaro-go/object v0.0.0-20241026212449-753ce10ec94c
	github.com/xaionaro-go/obs-grpc-proxy v0.0.0-20241018162120-5faf4e7a684a
	github.com/xaionaro-go/observability v0.0.0-20250420133500-5c4d2e045932
	github.com/xaionaro-go/player v0.0.0-20250427220051-e366ad8a1fb5
	github.com/xaionaro-go/recoder v0.0.0-20250503155018-6f353978d332
	github.com/xaionaro-go/secret v0.0.0-20250111141743-ced12e1082c2
	github.com/xaionaro-go/serializable v0.0.0-20250412140540-5ac572306599
	github.com/xaionaro-go/timeapiio v0.0.0-20240915203246-b907cf699af3
	github.com/xaionaro-go/typing v0.0.0-20221123235249-2229101d38ba
	github.com/xaionaro-go/unsafetools v0.0.0-20241024014258-a46e1ce3763e
	github.com/xaionaro-go/xcontext v0.0.0-20250111150717-e70e1f5b299c
	github.com/xaionaro-go/xfyne v0.0.0-20250615190411-4c96281f6e25
	github.com/xaionaro-go/xlogrus v0.0.0-20250111150201-60557109545a
	github.com/xaionaro-go/xpath v0.0.0-20250111145115-55f5728f643f
	github.com/xaionaro-go/xsync v0.0.0-20250614210231-b74f647f859f
	github.com/yutopp/go-flv v0.3.1
	golang.org/x/crypto v0.38.0
	google.golang.org/grpc v1.71.1
	google.golang.org/protobuf v1.36.6
	gopkg.in/yaml.v2 v2.4.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/BurntSushi/xgbutil v0.0.0-20190907113008-ad855c713046
	github.com/cloudwego/eino-ext/components/model/openai v0.0.0-20250424061409-ccd60fbc7c1c
	github.com/phuslu/goid v1.0.2 // indirect
	github.com/pion/datachannel v1.5.10 // indirect
	github.com/pion/dtls/v2 v2.2.12 // indirect
	github.com/pion/mdns v0.0.12 // indirect
	github.com/pion/randutil v0.1.0 // indirect
	github.com/pion/sctp v1.8.36 // indirect
	github.com/pion/srtp/v2 v2.0.20 // indirect
	github.com/pion/stun v0.6.1 // indirect
	github.com/pion/transport/v2 v2.2.10 // indirect
	github.com/pion/turn/v2 v2.1.6 // indirect
	github.com/pion/webrtc/v3 v3.3.0 // indirect
	github.com/tiendc/go-deepcopy v1.5.2
)
