module github.com/NorthAIProject/north-client

go 1.27.3

tool (
	github.com/pressly/goose/v3/cmd/goose
	mvdan.cc/gofumpt
)

require (
	github.com/FACorreiaa/go-utils v1.2.0
	github.com/SherClockHolmes/webpush-go v1.4.0
	github.com/anthropics/anthropic-sdk-go v1.84.0
	github.com/aws/aws-sdk-go-v2 v1.47.8
	github.com/aws/aws-sdk-go-v2/config v1.33.16
	github.com/aws/aws-sdk-go-v2/credentials v1.20.16
	github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager v0.5.5
	github.com/aws/aws-sdk-go-v2/service/s3 v1.114.9
	github.com/go-echarts/go-echarts/v2 v2.7.3
	github.com/go-webauthn/webauthn v0.18.2
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.12.0
	github.com/joho/godotenv v1.5.1
	github.com/labstack/echo/v5 v5.4.4
	github.com/ledongthuc/pdf v0.0.0-20261020125853-5e46805f718e
	github.com/modelcontextprotocol/go-sdk v1.8.0
	github.com/pgvector/pgvector-go v0.4.1
	github.com/posthog/posthog-go v1.42.0
	github.com/pressly/goose/v3 v3.29.0
	github.com/prometheus/client_golang v1.25.0
	github.com/xuri/excelize/v2 v2.11.1
	go.yaml.in/yaml/v3 v3.0.6
	golang.org/x/crypto v0.58.0
	golang.org/x/image v0.47.0
	golang.org/x/oauth2 v0.37.0
	golang.org/x/sync v0.24.0
	golang.org/x/text v0.43.0
	google.golang.org/api v0.311.0
	google.golang.org/genai v1.77.0
)

require (
	cloud.google.com/go v0.123.0 // indirect
	cloud.google.com/go/auth v0.24.3 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.3.1 // indirect
	cloud.google.com/go/compute/metadata v0.10.0 // indirect
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/azcore v1.23.3 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/azidentity v1.14.1 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/internal v1.13.0 // indirect
	github.com/AzureAD/microsoft-authentication-library-for-go v1.10.1 // indirect
	github.com/ClickHouse/ch-go v0.74.0 // indirect
	github.com/ClickHouse/clickhouse-go/v2 v2.49.0 // indirect
	github.com/andybalholm/brotli v1.2.8 // indirect
	github.com/antlr4-go/antlr/v4 v4.13.1 // indirect
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.26 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.20.8 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.5.11 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.8.11 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.5.11 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.24 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.11.12 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.14.11 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.20.11 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.12.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.39.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.44.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.51.9 // indirect
	github.com/aws/smithy-go v1.28.8 // indirect
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/buger/jsonparser v1.6.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/coder/websocket v1.8.15 // indirect
	github.com/dustin/go-humanize v1.1.0 // indirect
	github.com/elastic/go-sysinfo v1.15.5 // indirect
	github.com/elastic/go-windows v1.0.2 // indirect
	github.com/felixge/httpsnoop v1.1.0 // indirect
	github.com/fxamacker/cbor/v2 v2.10.0 // indirect
	github.com/go-faster/city v1.0.1 // indirect
	github.com/go-faster/errors v0.8.0 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-sql-driver/mysql v1.10.1 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/go-webauthn/x v0.3.1 // indirect
	github.com/goccy/go-json v0.12.0 // indirect
	github.com/golang-jwt/jwt/v4 v4.5.2 // indirect
	github.com/golang-sql/civil v0.0.0-20220223132316-b832511892a9 // indirect
	github.com/golang-sql/sqlexp v0.1.0 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/go-tpm v0.9.8 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/google/s2a-go v0.1.11 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.25 // indirect
	github.com/googleapis/gax-go/v2 v2.27.0 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/govalues/decimal v0.1.36 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/invopop/jsonschema v0.14.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.3 // indirect
	github.com/jonboulle/clockwork v0.5.0 // indirect
	github.com/klauspost/compress v1.20.3 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/mfridman/xflag v0.1.0 // indirect
	github.com/microsoft/go-mssqldb v1.11.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/ncruces/go-strftime v1.1.0 // indirect
	github.com/pandatix/go-abnf v0.5.5 // indirect
	github.com/paulmach/orb v0.13.0 // indirect
	github.com/pb33f/go-yaml v0.1.1 // indirect
	github.com/pb33f/ordered-map/v2 v2.3.2 // indirect
	github.com/philhofer/fwd v1.2.1 // indirect
	github.com/pierrec/lz4/v4 v4.1.33 // indirect
	github.com/pkg/browser v0.0.0-20240102092130-5ac0b6a4141c // indirect
	github.com/prometheus/client_model v0.6.3 // indirect
	github.com/prometheus/common v0.73.1 // indirect
	github.com/prometheus/procfs v0.23.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/richardlehane/mscfb v1.0.9 // indirect
	github.com/richardlehane/msoleps v1.0.6 // indirect
	github.com/rickb777/date/v2 v2.5.1 // indirect
	github.com/rickb777/period v1.3.0 // indirect
	github.com/rickb777/plural/v2 v2.1.1 // indirect
	github.com/segmentio/asm v1.2.1 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/sethvargo/go-retry v0.5.0 // indirect
	github.com/shopspring/decimal v1.5.0 // indirect
	github.com/standard-webhooks/standard-webhooks/libraries v0.0.1 // indirect
	github.com/stretchr/objx v0.8.0 // indirect
	github.com/stretchr/testify v1.12.1 // indirect
	github.com/tidwall/gjson v1.20.2 // indirect
	github.com/tidwall/match v1.2.0 // indirect
	github.com/tidwall/pretty v1.3.0 // indirect
	github.com/tidwall/sjson v1.2.8 // indirect
	github.com/tiendc/go-deepcopy v1.7.2 // indirect
	github.com/tinylib/msgp v1.6.5 // indirect
	github.com/tursodatabase/libsql-client-go v0.0.0-20260528064733-9d5d30a29a60 // indirect
	github.com/vertica/vertica-sql-go v1.3.8 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/xuri/efp v0.0.3-0.20260924042702-217b81952dec // indirect
	github.com/xuri/nfp v0.0.2-0.20250530014748-2ddeb826f9a9 // indirect
	github.com/yandex-cloud/go-genproto v0.126.0 // indirect
	github.com/ydb-platform/ydb-go-genproto v0.0.0-20260810123728-f0c151ab31b9 // indirect
	github.com/ydb-platform/ydb-go-sdk-auth-environ v0.5.3 // indirect
	github.com/ydb-platform/ydb-go-sdk/v3 v3.155.1 // indirect
	github.com/ydb-platform/ydb-go-yc v0.12.5 // indirect
	github.com/ydb-platform/ydb-go-yc-metadata v0.6.2 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	github.com/ziutek/mymysql v1.5.4 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.72.0 // indirect
	go.opentelemetry.io/otel v1.47.0 // indirect
	go.opentelemetry.io/otel/log v1.47.0 // indirect
	go.opentelemetry.io/otel/metric v1.47.0 // indirect
	go.opentelemetry.io/otel/trace v1.47.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/exp v0.0.0-20261106192419-47ce03d25ef0 // indirect
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/net v0.62.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/time v0.16.0 // indirect
	golang.org/x/tools v0.52.1-0.20261106145633-d407941a339a // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20261012220534-4341edf3f941 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20261104224025-d1e00e9f33ab // indirect
	google.golang.org/grpc v1.85.0 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
	howett.net/plist v1.0.1 // indirect
	modernc.org/libc v1.78.0 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
	modernc.org/sqlite v1.60.1 // indirect
	mvdan.cc/gofumpt v0.11.0 // indirect
)
