module creative-mode/harness

go 1.24.3

replace github.com/coreycole/creative-mode/pkg/worldchannel => ../pkg/worldchannel

replace github.com/coreycole/creative-mode/pkg/imagegen => ../pkg/imagegen

replace github.com/coreycole/creative-mode/pkg/mayorchat => ../pkg/mayorchat

replace github.com/coreycole/creative-mode/pkg/markdown => ../pkg/markdown

require (
	github.com/a-h/templ v0.3.977
	github.com/anthropics/anthropic-sdk-go v1.22.1
	github.com/bwmarrin/discordgo v0.28.1
	github.com/coreycole/creative-mode/pkg/imagegen v0.0.0-00010101000000-000000000000
	github.com/coreycole/creative-mode/pkg/markdown v0.0.0-00010101000000-000000000000
	github.com/coreycole/creative-mode/pkg/mayorchat v0.0.0-00010101000000-000000000000
	github.com/coreycole/creative-mode/pkg/worldchannel v0.0.0-20260215081125-686d34b3d948
	github.com/coreycole/datastarui v0.0.0-20260131230526-8815ff5a1c48
	github.com/google/uuid v1.6.0
	github.com/labstack/echo/v4 v4.15.0
	github.com/mattn/go-sqlite3 v1.14.34
	github.com/starfederation/datastar-go v1.1.0
	golang.org/x/net v0.50.0
)

require (
	buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go v1.36.10-20250912141014-52f32327d4b0.1 // indirect
	cloud.google.com/go v0.116.0 // indirect
	cloud.google.com/go/auth v0.9.3 // indirect
	cloud.google.com/go/compute/metadata v0.6.0 // indirect
	connectrpc.com/connect v1.19.1 // indirect
	github.com/CAFxX/httpcompression v0.0.9 // indirect
	github.com/Oudwins/tailwind-merge-go v0.2.0 // indirect
	github.com/alecthomas/chroma/v2 v2.23.1 // indirect
	github.com/andybalholm/brotli v1.2.0 // indirect
	github.com/dlclark/regexp2 v1.11.5 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/gomarkdown/markdown v0.0.0-20250810172220-2e2c11897d1a // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/s2a-go v0.1.8 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.4 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/klauspost/compress v1.18.0 // indirect
	github.com/labstack/gommon v0.4.2 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/tidwall/gjson v1.18.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasttemplate v1.2.2 // indirect
	go.opencensus.io v0.24.0 // indirect
	golang.org/x/crypto v0.48.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	golang.org/x/text v0.34.0 // indirect
	golang.org/x/time v0.14.0 // indirect
	google.golang.org/genai v1.46.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250603155806-513f23925822 // indirect
	google.golang.org/grpc v1.71.0 // indirect
	google.golang.org/protobuf v1.36.10 // indirect
)
