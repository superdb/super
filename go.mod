module github.com/superdb/super

go 1.27

require (
	github.com/RoaringBitmap/roaring/v2 v2.9.0
	github.com/agnivade/levenshtein v1.1.1
	github.com/alecthomas/units v0.0.0-20190924025748-f65c72e2690d
	github.com/apache/arrow-go/v18 v18.8.0
	github.com/araddon/dateparse v0.0.0-20210429162001-6b43995a97de
	github.com/aws/aws-sdk-go v1.36.17
	github.com/axiomhq/hyperloglog v0.2.5
	github.com/bytedance/sonic v1.15.3-0.20260730064818-2a36d6da63e2
	github.com/go-redis/redis/v8 v8.11.5
	github.com/goccy/go-yaml v1.19.0
	github.com/golang-jwt/jwt/v4 v4.5.2
	github.com/gorilla/mux v1.7.5-0.20200711200521-98cb6bf42e08
	github.com/gosuri/uilive v0.0.4
	github.com/hashicorp/golang-lru/arc/v2 v2.0.7
	github.com/kr/text v0.2.0
	github.com/lestrrat-go/strftime v1.0.6
	github.com/paulbellamy/ratecounter v0.2.0
	github.com/pbnjay/memory v0.0.0-20190104145345-974d429e7ae4
	github.com/peterh/liner v1.1.0
	github.com/pierrec/lz4/v4 v4.1.29
	github.com/pkg/browser v0.0.0-20210911075715-681adbf594b8
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2
	github.com/prometheus/client_golang v1.14.0
	github.com/prometheus/client_model v0.3.0
	github.com/ronanh/intcomp v1.1.1
	github.com/rs/cors v1.8.0
	github.com/shellyln/go-sql-like-expr v0.0.1
	github.com/stretchr/testify v1.12.1
	github.com/teamortix/golang-wasm/wasm v0.0.0-20230719150929-5d000994c833
	github.com/x448/float16 v0.8.4
	github.com/yuin/goldmark v1.4.13
	go.uber.org/mock v0.5.1
	go.uber.org/zap v1.23.0
	golang.org/x/exp v0.0.0-20260112195511-716be5621a96
	golang.org/x/sync v0.22.0
	golang.org/x/sys v0.47.0
	golang.org/x/term v0.45.0
	golang.org/x/text v0.41.0
	gopkg.in/natefinch/lumberjack.v2 v2.0.0
)

require (
	github.com/andybalholm/brotli v1.2.3 // indirect
	github.com/apache/thrift v0.24.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/bits-and-blooms/bitset v1.12.0 // indirect
	github.com/bytedance/gopkg v0.1.3 // indirect
	github.com/bytedance/sonic/loader v0.5.2 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/cloudwego/base64x v0.1.6 // indirect
	github.com/dgryski/go-metro v0.0.0-20180109044635-280f6062b5bc // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/goccy/go-json v0.10.6 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/flatbuffers v25.12.19+incompatible // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/kamstrup/intmap v0.5.1 // indirect
	github.com/klauspost/compress v1.19.2 // indirect
	github.com/klauspost/cpuid/v2 v2.4.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/mattn/go-runewidth v0.0.20 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.1 // indirect
	github.com/mna/pigeon v1.2.1 // indirect
	github.com/mschoch/smat v0.2.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/prometheus/common v0.37.0 // indirect
	github.com/prometheus/procfs v0.8.0 // indirect
	github.com/twitchyliquid64/golang-asm v0.15.1 // indirect
	github.com/zeebo/xxh3 v1.1.0 // indirect
	go.uber.org/atomic v1.7.0 // indirect
	go.uber.org/multierr v1.8.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/arch v0.0.0-20210923205945-b76863e36670 // indirect
	golang.org/x/mod v0.38.0 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/telemetry v0.0.0-20260708182218-49f421fb7959 // indirect
	golang.org/x/tools v0.48.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/grpc v1.83.2 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
)

tool (
	github.com/mna/pigeon
	go.uber.org/mock/mockgen
	golang.org/x/tools/cmd/goimports
)
