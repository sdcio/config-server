protoc -I . -I ./vendor --gogo_opt=paths=source_relative --gogo_out=. apis/condition/v1alpha1/generated.proto

protoc -I . -I ./vendor -I ./apis/condition/v1alpha1 --gogo_opt=paths=source_relative --gogo_out=. apis/config/v1alpha1/generated.proto


go run ./tools/apiserver-runtime-gen \
    -g applyconfiguration-gen \
    --module github.com/sdcio/config-server