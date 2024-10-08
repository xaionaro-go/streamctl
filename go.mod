module github.com/xaionaro-go/streamctl

go 1.22.1

toolchain go1.22.3

// The original go-yaml is very slow, using the improved version instead
replace github.com/goccy/go-yaml v1.11.3 => github.com/yoelsusanto/go-yaml v0.0.0-20240324162521-2018c1ab915b

replace github.com/yutopp/go-rtmp v0.0.7 => ../go-rtmp/

replace github.com/xaionaro-go/mediamtx v0.0.0-20241007163135-aa6b3d02fd68 => ../mediamtx/

replace code.cloudfoundry.org/bytefmt => github.com/cloudfoundry/bytefmt v0.0.0-20211005130812-5bb3c17173e5

replace github.com/xaionaro-go/obs-grpc-proxy v0.0.0-20240826003505-317c16ed8b69 => ../obs-grpc-proxy/

replace github.com/andreykaipov/goobs v1.4.1 => ../goobs/

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
	github.com/Jorropo/jsync v1.0.1 // indirect
	github.com/MicahParks/jwkset v0.5.20 // indirect
	github.com/MicahParks/keyfunc/v3 v3.3.5 // indirect
	github.com/Microsoft/go-winio v0.6.1 // indirect
	github.com/ProtonMail/go-crypto v1.0.0 // indirect
	github.com/abema/go-mp4 v1.2.0 // indirect
	github.com/asticode/go-astits v1.13.0 // indirect
	github.com/benbjohnson/clock v1.3.5 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/bluenviron/gohlslib/v2 v2.0.0-20241007134735-ed88408cd4de // indirect
	github.com/bluenviron/gortsplib/v4 v4.10.7-0.20241007135843-2ca0bffa20a2 // indirect
	github.com/bluenviron/mediacommon v1.12.5-0.20241007134151-8883ba897cfc // indirect
	github.com/buger/jsonparser v1.1.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cloudflare/circl v1.3.7 // indirect
	github.com/containerd/cgroups v1.1.0 // indirect
	github.com/coreos/go-systemd/v22 v22.5.0 // indirect
	github.com/cyphar/filepath-securejoin v0.2.4 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/davidlazar/go-crypto v0.0.0-20200604182044-b73af7476f6c // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.2.0 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/elastic/gosigar v0.14.2 // indirect
	github.com/emirpasic/gods v1.18.1 // indirect
	github.com/fatih/color v1.10.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/flynn/noise v1.1.0 // indirect
	github.com/francoispqt/gojay v1.2.13 // indirect
	github.com/fredbi/uri v1.1.0 // indirect
	github.com/fsnotify/fsnotify v1.7.0 // indirect
	github.com/fyne-io/gl-js v0.0.0-20230506162202-1fdaa286a934 // indirect
	github.com/fyne-io/glfw-js v0.0.0-20240101223322-6e1efdc71b7a // indirect
	github.com/fyne-io/image v0.0.0-20240417123036-dc0ee9e7c964 // indirect
	github.com/gen2brain/shm v0.0.0-20230802011745-f2460f5984f7 // indirect
	github.com/go-git/gcfg v1.5.1-0.20230307220236-3a3c6141e376 // indirect
	github.com/go-gl/gl v0.0.0-20231021071112-07e5d0ea2e71 // indirect
	github.com/go-gl/glfw/v3.3/glfw v0.0.0-20240506104042-037f3cc74f2a // indirect
	github.com/go-logr/logr v1.4.2 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ng/slices v0.0.0-20230703171042-6195d35636a2 // indirect
	github.com/go-ng/sort v0.0.0-20220617173827-2cc7cd04f7c7 // indirect
	github.com/go-ng/xatomic v0.0.0-20230519181013-85c0ec87e55f // indirect
	github.com/go-ng/xsort v0.0.0-20220617174223-1d146907bccc // indirect
	github.com/go-redis/redis/v7 v7.2.0 // indirect
	github.com/go-task/slim-sprig v0.0.0-20230315185526-52ccab3ef572 // indirect
	github.com/go-text/render v0.1.0 // indirect
	github.com/go-text/typesetting v0.1.1 // indirect
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang-jwt/jwt/v4 v4.0.0 // indirect
	github.com/golang-jwt/jwt/v5 v5.2.1 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/google/btree v1.1.2 // indirect
	github.com/google/gopacket v1.1.19 // indirect
	github.com/google/pprof v0.0.0-20240207164012-fb44976bdcd5 // indirect
	github.com/google/s2a-go v0.1.8 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.2 // indirect
	github.com/googleapis/gax-go/v2 v2.13.0 // indirect
	github.com/gookit/color v1.5.4 // indirect
	github.com/gopherjs/gopherjs v1.17.2 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/golang-lru v1.0.2 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.5 // indirect
	github.com/hashicorp/hcl v1.0.0 // indirect
	github.com/hashicorp/logutils v1.0.0 // indirect
	github.com/huandu/go-tls v0.0.0-20200109070953-6f75fb441850 // indirect
	github.com/huin/goupnp v1.3.0 // indirect
	github.com/iguanesolutions/go-systemd/v5 v5.1.1 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/ipfs/boxo v0.13.1 // indirect
	github.com/ipfs/go-cid v0.4.1 // indirect
	github.com/ipfs/go-datastore v0.6.0 // indirect
	github.com/ipfs/go-log v1.0.5 // indirect
	github.com/ipfs/go-log/v2 v2.5.1 // indirect
	github.com/ipld/go-ipld-prime v0.21.0 // indirect
	github.com/jackpal/go-nat-pmp v1.0.2 // indirect
	github.com/jbenet/go-context v0.0.0-20150711004518-d14ea06fba99 // indirect
	github.com/jbenet/go-temp-err-catcher v0.1.0 // indirect
	github.com/jbenet/goprocess v0.1.4 // indirect
	github.com/jeandeaual/go-locale v0.0.0-20240223122105-ce5225dcaa49 // indirect
	github.com/jezek/xgb v1.1.0 // indirect
	github.com/jsummers/gobmp v0.0.0-20230614200233-a9de23ed2e25 // indirect
	github.com/kballard/go-shellquote v0.0.0-20180428030007-95032a82bc51 // indirect
	github.com/kevinburke/ssh_config v1.2.0 // indirect
	github.com/klauspost/compress v1.17.7 // indirect
	github.com/klauspost/cpuid/v2 v2.2.7 // indirect
	github.com/koron/go-ssdp v0.0.4 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/libp2p/go-buffer-pool v0.1.0 // indirect
	github.com/libp2p/go-cidranger v1.1.0 // indirect
	github.com/libp2p/go-flow-metrics v0.1.0 // indirect
	github.com/libp2p/go-libp2p-asn-util v0.4.1 // indirect
	github.com/libp2p/go-libp2p-kbucket v0.6.3 // indirect
	github.com/libp2p/go-libp2p-record v0.2.0 // indirect
	github.com/libp2p/go-libp2p-routing-helpers v0.7.2 // indirect
	github.com/libp2p/go-msgio v0.3.0 // indirect
	github.com/libp2p/go-nat v0.2.0 // indirect
	github.com/libp2p/go-netroute v0.2.1 // indirect
	github.com/libp2p/go-reuseport v0.4.0 // indirect
	github.com/libp2p/go-yamux/v4 v4.0.1 // indirect
	github.com/lxn/win v0.0.0-20210218163916-a377121e959e // indirect
	github.com/magiconair/properties v1.8.5 // indirect
	github.com/marten-seemann/tcp v0.0.0-20210406111302-dfbc87cc63fd // indirect
	github.com/matthewhartstonge/argon2 v1.0.1 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/miekg/dns v1.1.59 // indirect
	github.com/mikioh/tcpinfo v0.0.0-20190314235526-30a79bb1804b // indirect
	github.com/mikioh/tcpopt v0.0.0-20190314235656-172688c1accc // indirect
	github.com/minio/sha256-simd v1.0.1 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/mmcloughlin/profile v0.1.1 // indirect
	github.com/mr-tron/base58 v1.2.0 // indirect
	github.com/multiformats/go-base32 v0.1.0 // indirect
	github.com/multiformats/go-base36 v0.2.0 // indirect
	github.com/multiformats/go-multiaddr-dns v0.3.1 // indirect
	github.com/multiformats/go-multiaddr-fmt v0.1.0 // indirect
	github.com/multiformats/go-multibase v0.2.0 // indirect
	github.com/multiformats/go-multicodec v0.9.0 // indirect
	github.com/multiformats/go-multihash v0.2.3 // indirect
	github.com/multiformats/go-multistream v0.5.0 // indirect
	github.com/multiformats/go-varint v0.0.7 // indirect
	github.com/nicksnyder/go-i18n/v2 v2.4.0 // indirect
	github.com/nu7hatch/gouuid v0.0.0-20131221200532-179d4d0c4d8d // indirect
	github.com/onsi/ginkgo v1.16.5 // indirect
	github.com/onsi/ginkgo/v2 v2.15.0 // indirect
	github.com/opencontainers/runtime-spec v1.2.0 // indirect
	github.com/opentracing/opentracing-go v1.2.0 // indirect
	github.com/patrickmn/go-cache v2.1.0+incompatible // indirect
	github.com/pbnjay/memory v0.0.0-20210728143218-7b4eea64cf58 // indirect
	github.com/pelletier/go-toml v1.9.3 // indirect
	github.com/pion/randutil v0.1.0 // indirect
	github.com/pion/rtcp v1.2.14 // indirect
	github.com/pion/rtp v1.8.9 // indirect
	github.com/pion/sdp/v3 v3.0.9 // indirect
	github.com/pjbgf/sha1cd v0.3.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/polydawn/refmt v0.89.0 // indirect
	github.com/prometheus/client_model v0.6.0 // indirect
	github.com/prometheus/common v0.47.0 // indirect
	github.com/prometheus/procfs v0.12.0 // indirect
	github.com/quic-go/qpack v0.4.0 // indirect
	github.com/quic-go/quic-go v0.42.0 // indirect
	github.com/quic-go/webtransport-go v0.6.0 // indirect
	github.com/raulk/go-watchdog v1.3.0 // indirect
	github.com/rogpeppe/go-internal v1.11.0 // indirect
	github.com/rymdport/portal v0.2.2 // indirect
	github.com/samber/lo v1.36.0 // indirect
	github.com/satori/go.uuid v1.2.0 // indirect
	github.com/sergi/go-diff v1.3.2-0.20230802210424-5b0b94c5c0d3 // indirect
	github.com/sigurn/crc16 v0.0.0-20240131213347-83fcde1e29d1 // indirect
	github.com/sigurn/crc8 v0.0.0-20220107193325-2243fe600f9f // indirect
	github.com/skeema/knownhosts v1.2.2 // indirect
	github.com/songgao/water v0.0.0-20200317203138-2b4b6d7c09d8 // indirect
	github.com/spaolacci/murmur3 v1.1.0 // indirect
	github.com/spf13/afero v1.6.0 // indirect
	github.com/spf13/cast v1.3.1 // indirect
	github.com/spf13/jwalterweatherman v1.1.0 // indirect
	github.com/srwiley/oksvg v0.0.0-20221011165216-be6e8873101c // indirect
	github.com/srwiley/rasterx v0.0.0-20220730225603-2ab79fcdd4ef // indirect
	github.com/subosito/gotenv v1.2.0 // indirect
	github.com/vishvananda/netlink v1.1.1-0.20211118161826-650dca95af54 // indirect
	github.com/vishvananda/netns v0.0.4 // indirect
	github.com/whyrusleeping/go-keyspace v0.0.0-20160322163242-5b898ac5add1 // indirect
	github.com/xaionaro-go/spinlock v0.0.0-20200518175509-30e6d1ce68a1 // indirect
	github.com/xanzy/ssh-agent v0.3.3 // indirect
	github.com/xo/terminfo v0.0.0-20210125001918-ca9a967f8778 // indirect
	github.com/yuin/goldmark v1.7.1 // indirect
	github.com/yutopp/go-amf0 v0.1.0 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.49.0 // indirect
	go.opentelemetry.io/otel v1.24.0 // indirect
	go.opentelemetry.io/otel/metric v1.24.0 // indirect
	go.opentelemetry.io/otel/trace v1.24.0 // indirect
	go.uber.org/dig v1.17.1 // indirect
	go.uber.org/fx v1.20.1 // indirect
	go.uber.org/mock v0.4.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
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
	golang.zx2c4.com/wintun v0.0.0-20230126152724-0fa3db229ce2 // indirect
	golang.zx2c4.com/wireguard v0.0.0-20231211153847-12269c276173 // indirect
	gonum.org/v1/gonum v0.14.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240730163845-b1a4ccb954bf // indirect
	gopkg.in/ini.v1 v1.67.0 // indirect
	gopkg.in/natefinch/npipe.v2 v2.0.0-20160621034901-c1b8fa8bdcce // indirect
	gopkg.in/warnings.v0 v0.1.2 // indirect
	gvisor.dev/gvisor v0.0.0-20230927004350-cbd86285d259 // indirect
	lukechampine.com/blake3 v1.2.1 // indirect
)

require (
	fyne.io/fyne/v2 v2.5.0
	github.com/AgustinSRG/go-child-process-manager v1.0.1
	github.com/AlexxIT/go2rtc v1.9.4
	github.com/DataDog/gostackparse v0.6.0
	github.com/DexterLB/mpvipc v0.0.0-20230829142118-145d6eabdc37
	github.com/adrg/libvlc-go/v3 v3.1.5
	github.com/andreykaipov/goobs v1.4.1
	github.com/anthonynsimon/bild v0.14.0
	github.com/asaskevich/EventBus v0.0.0-20200907212545-49d423059eef
	github.com/asticode/go-astiav v0.19.0
	github.com/asticode/go-astikit v0.42.0
	github.com/bamiaux/rez v0.0.0-20170731184118-29f4463c688b
	github.com/blang/mpv v0.0.0-20160810175505-d56d7352e068
	github.com/chai2010/webp v1.1.1
	github.com/dustin/go-humanize v1.0.1
	github.com/getsentry/sentry-go v0.28.1
	github.com/go-git/go-git/v5 v5.12.0
	github.com/go-ng/xmath v0.0.0-20230704233441-028f5ea62335
	github.com/go-yaml/yaml v2.1.0+incompatible
	github.com/google/uuid v1.6.0
	github.com/gwuhaolin/livego v0.0.0-20220914133149-42d7596e8048
	github.com/hyprspace/hyprspace v0.10.1
	github.com/immune-gmbh/attestation-sdk v0.0.0-20230711173209-f44e4502aeca
	github.com/kbinani/screenshot v0.0.0-20230812210009-b87d31814237
	github.com/libp2p/go-libp2p v0.33.2
	github.com/libp2p/go-libp2p-kad-dht v0.25.2
	github.com/lusingander/colorpicker v0.7.3
	github.com/multiformats/go-multiaddr v0.12.3
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.18.0
	github.com/rs/zerolog v1.33.0
	github.com/sethvargo/go-password v0.3.1
	github.com/sirupsen/logrus v1.9.3
	github.com/spf13/pflag v1.0.5
	github.com/spf13/viper v1.8.1
	github.com/stretchr/testify v1.9.0
	github.com/xaionaro-go/datacounter v1.0.4
	github.com/xaionaro-go/gorex v0.0.0-20200314172213-23bed04bc3e3
	github.com/xaionaro-go/lockmap v0.0.0-20240901172806-e17aea364748
	github.com/xaionaro-go/mediamtx v0.0.0-20241007163135-aa6b3d02fd68
	github.com/xaionaro-go/obs-grpc-proxy v0.0.0-20240826003505-317c16ed8b69
	github.com/xaionaro-go/timeapiio v0.0.0-20240915203246-b907cf699af3
	github.com/xaionaro-go/typing v0.0.0-20221123235249-2229101d38ba
	github.com/xaionaro-go/unsafetools v0.0.0-20210722164218-75ba48cf7b3c
	github.com/yl2chen/cidranger v1.0.2
	github.com/yutopp/go-flv v0.3.1
	github.com/yutopp/go-rtmp v0.0.7
	golang.org/x/crypto v0.28.0
	golang.org/x/net v0.30.0
	google.golang.org/grpc v1.65.0
	google.golang.org/protobuf v1.34.2
	gopkg.in/yaml.v2 v2.4.0
	gopkg.in/yaml.v3 v3.0.1
)
