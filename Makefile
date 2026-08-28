IMG ?= controller:latest

LOCALBIN ?= $(shell pwd)/bin
ENVTEST ?= $(LOCALBIN)/setup-envtest
ENVTEST_K8S_VERSION ?= 1.32.0
KUSTOMIZE ?= $(LOCALBIN)/kustomize

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: fmt vet ## Run unit tests (fast, no external binaries required).
	go test ./... -coverprofile cover.out

.PHONY: envtest
envtest: $(LOCALBIN) ## Download the setup-envtest tool.
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

.PHONY: test-integration
test-integration: manifests fmt vet envtest ## Run integration tests against a real envtest kube-apiserver.
	KUBEBUILDER_ASSETS="$$($(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" \
		go test ./internal/controller/... -tags=integration -run TestIntegration -v -timeout 5m

$(LOCALBIN):
	mkdir -p $(LOCALBIN)

.PHONY: kustomize
kustomize: $(LOCALBIN) ## Download the kustomize tool.
	test -s $(KUSTOMIZE) || GOBIN=$(LOCALBIN) go install sigs.k8s.io/kustomize/kustomize/v5@latest

.PHONY: build
build: fmt vet ## Build manager binary.
	go build -o bin/manager cmd/main.go

.PHONY: run
run: fmt vet ## Run the manager against the currently configured Kubernetes cluster in ~/.kube/config.
	go run ./cmd/main.go

.PHONY: docker-build
docker-build: ## Build the manager container image.
	docker build -t ${IMG} .

.PHONY: docker-push
docker-push: ## Push the manager container image.
	docker push ${IMG}

.PHONY: install
install: ## Install CRDs into the cluster configured in ~/.kube/config.
	kubectl apply -f config/crd/bases

.PHONY: uninstall
uninstall: ## Uninstall CRDs from the cluster configured in ~/.kube/config.
	kubectl delete -f config/crd/bases --ignore-not-found

.PHONY: deploy
deploy: kustomize ## Deploy the controller to the cluster configured in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | kubectl apply -f -

.PHONY: undeploy
undeploy: kustomize ## Undeploy the controller from the cluster configured in ~/.kube/config.
	$(KUSTOMIZE) build config/default | kubectl delete --ignore-not-found -f -

.PHONY: manifests
manifests: ## Regenerate CRD YAML from Go type markers (requires controller-gen).
	controller-gen crd rbac:roleName=stackit-compute-operator-manager-role paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: ## Regenerate zz_generated.deepcopy.go from Go type markers (requires controller-gen).
	controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./..."
