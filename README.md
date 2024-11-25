# Config-server

The config-server is a Kubernetes-based Operator and comprises of several controllers:

- Schema Controller: Manages the lifecycle of schemas using Schema Custom Resources (CR).
- Discovery Controller: Manages the lifecycle of targets through DiscoveryRule CR, discovering devices/NF(s)
- Target Controller: Manages the lifecycle of Target DataStores using Target CR.
- Config API Server: Manages the lifecycle of Config resources.
    - Utilizes its storage backend (not etcd).
    - Interacts declaratively with the data-server through Intent transactions.
    - Implements validation checks, rejecting configurations that fail validation.



/Users/henderiw/go/bin/go-to-protobuf --go-header-file hack/boilerplate.go.txt --packages +github.com/sdcio/config-server/apis/config/v1alpha1 --apimachinery-packages -k8s.io/apimachinery/pkg/api/resource,-k8s.io/apimachinery/pkg/runtime/schema,-k8s.io/apimachinery/pkg/runtime,-k8s.io/apimachinery/pkg/apis/meta/v1


 /Users/henderiw/code/tmp/code-generator/cmd/go-to-protobuf/go-to-protobuf --go-header-file hack/boilerplate.go.txt --packages +github.com/sdcio/config-server/apis/config/v1alpha1 --apimachinery-packages -k8s.io/apimachinery/pkg/api/resource,-k8s.io/apimachinery/pkg/runtime/schema,-k8s.io/apimachinery/pkg/runtime,-k8s.io/apimachinery/pkg/apis/meta/v1


 protoc -I . -I . --gogo_out=. github.com/sdcio/config-server/apis/config/v1alpha1/generated.proto --gogo_opt=paths=source_relative



protoc -I . -I ./vendor --proto_path=apis/config/v1alpha1 --gogo_out=paths=source_relative:apis/config/v1alpha1 apis/config/v1alpha1/generated.proto 

protoc -I . -I ./vendor --gogo_out=. github.com/sdcio/config-server/apis/config/v1alpha1/generated.proto


protoc -I . -I ./vendor --proto_path=apis/config/v1alpha1 --gogo_out=paths=source_relative:github.com/sdcio/config-server/apis/config/v1alpha1 github.com/sdcio/config-server/apis/config/v1alpha1/generated.proto 


protoc -I . -I ./vendor --gogo_out=paths=source_relative:github.com/sdcio/config-server/apis/condition/v1alpha1 apis/condition/v1alpha1/generated.proto


protoc -I . -I ./vendor --gogo_out=paths=source_relative:apis/condition/v1alpha1 apis/condition/v1alpha1/generated.proto