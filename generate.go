package chaosmesh

//go:generate go tool client-gen --input=github.com/chaos-mesh/chaos-mesh/api/v1alpha1 --input-base= --output-dir=./pkg/generated --output-pkg=github.com/chaos-mesh/chaos-mesh/pkg/generated/ --clientset-name=versioned --go-header-file=./hack/boilerplate/boilerplate.generatego.txt --fake-clientset=true -v=2
