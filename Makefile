.PHONY: build
build:
	go build -o run-controller cmd/controller/main.go

.PHONY: run
run:
	SYSTEM_NAMESPACE=kube-system go run cmd/controller/main.go


.PHONY: ko
ko:
	KO_DOCKER_REPO=ghcr.io/garethjevans/run-controller ko build
