module github.com/llmariner/inference-manager

go 1.23.3

toolchain go1.23.4

require (
	github.com/aws/aws-sdk-go-v2 v1.32.3
	github.com/aws/aws-sdk-go-v2/feature/s3/manager v1.17.25
	github.com/aws/aws-sdk-go-v2/service/s3 v1.63.3
	github.com/aws/smithy-go v1.22.0
	github.com/go-logr/logr v1.4.2
	github.com/go-logr/stdr v1.2.2
	github.com/google/go-cmp v0.6.0
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.22.0
	github.com/kedacore/keda/v2 v2.16.0
	github.com/llmariner/api-usage v1.11.0
	github.com/llmariner/cluster-manager v1.5.1
	github.com/llmariner/common v0.19.0
	github.com/llmariner/model-manager v1.18.0
	github.com/llmariner/rbac-manager v1.14.0
	github.com/llmariner/vector-store-manager v0.50.0
	github.com/prometheus/client_golang v1.21.0
	github.com/redis/go-redis/v9 v9.7.3
	github.com/spf13/cobra v1.9.1
	github.com/stretchr/testify v1.10.0
	golang.org/x/sync v0.12.0
	google.golang.org/genproto/googleapis/api v0.0.0-20241216192217-9240e9c98484
	google.golang.org/grpc v1.67.1
	google.golang.org/protobuf v1.36.5
	gopkg.in/yaml.v3 v3.0.1
	k8s.io/api v0.31.3
	k8s.io/apimachinery v0.31.3
	k8s.io/client-go v0.31.3
	k8s.io/utils v0.0.0-20240921022957-49e7df575cb6
	sigs.k8s.io/controller-runtime v0.19.1
	sigs.k8s.io/yaml v1.4.0
)

require (
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.6.6 // indirect
	github.com/aws/aws-sdk-go-v2/config v1.28.1 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.17.42 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.16.18 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.3.22 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.6.22 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.8.1 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.3.18 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.12.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.3.20 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.12.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.17.18 // indirect
	github.com/aws/aws-sdk-go-v2/service/kms v1.37.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.24.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.28.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.32.3 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/emicklei/go-restful/v3 v3.12.1 // indirect
	github.com/evanphx/json-patch/v5 v5.9.0 // indirect
	github.com/expr-lang/expr v1.17.0 // indirect
	github.com/fsnotify/fsnotify v1.7.0 // indirect
	github.com/fxamacker/cbor/v2 v2.7.0 // indirect
	github.com/go-openapi/jsonpointer v0.21.0 // indirect
	github.com/go-openapi/jsonreference v0.21.0 // indirect
	github.com/go-openapi/swag v0.23.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/gnostic-models v0.6.8 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/imdario/mergo v0.3.16 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/compress v1.17.11 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.62.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	github.com/spf13/pflag v1.0.6 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	golang.org/x/exp v0.0.0-20240808152545-0cdaa3abc0fa // indirect
	golang.org/x/net v0.38.0 // indirect
	golang.org/x/oauth2 v0.27.0 // indirect
	golang.org/x/sys v0.31.0 // indirect
	golang.org/x/term v0.30.0 // indirect
	golang.org/x/text v0.23.0 // indirect
	golang.org/x/time v0.7.0 // indirect
	gomodules.xyz/jsonpatch/v2 v2.4.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20241216192217-9240e9c98484 // indirect
	gopkg.in/evanphx/json-patch.v4 v4.12.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	k8s.io/apiextensions-apiserver v0.31.2 // indirect
	k8s.io/klog/v2 v2.130.1 // indirect
	k8s.io/kube-openapi v0.0.0-20241009091222-67ed5848f094 // indirect
	knative.dev/pkg v0.0.0-20241026180704-25f6002b00f3 // indirect
	sigs.k8s.io/json v0.0.0-20221116044647-bc3834ca7abd // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.4.1 // indirect
)
