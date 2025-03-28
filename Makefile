PROJECT_NAME := Pulumi SDK
SDKS         := dotnet nodejs python go
SUB_PROJECTS := $(SDKS:%=sdk/%)

include build/common.mk

PROJECT         := github.com/pulumi/pulumi/pkg/v3/cmd/pulumi

PKG_CODEGEN := github.com/pulumi/pulumi/pkg/v3/codegen
# nodejs and python codegen tests are much slower than go/dotnet:
PROJECT_PKGS    := $(shell cd ./pkg && go list ./... | grep -v -E '^${PKG_CODEGEN}/(dotnet|go|nodejs|python)')
INTEGRATION_PKG := github.com/pulumi/pulumi/tests/integration
TESTS_PKGS      := $(shell cd ./tests && go list -tags all ./... | grep -v tests/templates | grep -v ^${INTEGRATION_PKG}$)
VERSION         := $(if ${PULUMI_VERSION},${PULUMI_VERSION},$(shell ./scripts/pulumi-version.sh))

ifeq ($(DEBUG),"true")
$(info    SHELL           = ${SHELL})
$(info    VERSION         = ${VERSION})
endif

# Motivation: running `make TEST_ALL_DEPS= test_all` permits running
# `test_all` without the dependencies.
TEST_ALL_DEPS ?= build $(SUB_PROJECTS:%=%_install)

GO_TEST      = $(PYTHON) ../scripts/go-test.py $(GO_TEST_FLAGS)
GO_TEST_FAST = $(PYTHON) ../scripts/go-test.py $(GO_TEST_FAST_FLAGS)

ensure: .ensure.phony go.ensure $(SUB_PROJECTS:%=%_ensure)
.ensure.phony: sdk/go.mod pkg/go.mod tests/go.mod
	cd sdk && go mod download
	cd pkg && go mod download
	cd tests && go mod download
	@touch .ensure.phony

.PHONY: build-proto
PROTO_FILES := $(sort $(shell find proto/ -type f -name '*.proto') proto/generate.sh proto/build-container/Dockerfile $(wildcard proto/build-container/scripts/*))
build-proto:
	@printf "Protobuffer interfaces are ....... "
	@if [ "$$(cat proto/.checksum.txt)" = "$$(cksum $(PROTO_FILES))" ]; then \
		printf "\033[0;32mup to date\033[0m\n"; \
	else \
		printf "\033[0;34mout of date: REBUILDING\033[0m\n"; \
		cd proto && ./generate.sh || exit 1; \
		cd ../ && cksum $(PROTO_FILES) > proto/.checksum.txt; \
		printf "\033[0;34mProtobuffer interfaces have been \033[0;32mREBUILT\033[0m\n"; \
	fi

.PHONY: check-proto
check-proto:
	@if [ "$$(cat proto/.checksum.txt)" != "$$(cksum $(PROTO_FILES))" ]; then \
		echo "Protobuff checksum doesn't match. Run \`make build-proto\` to rebuild."; \
		exit 1; \
	fi

.PHONY: generate
generate::
	$(call STEP_MESSAGE)
	echo "This command does not do anything anymore. It will be removed in a future version."

ifeq ($(PULUMI_TEST_COVERAGE_PATH),)
build:: build-proto go.ensure
	cd pkg && go install -ldflags "-X github.com/pulumi/pulumi/pkg/v3/version.Version=${VERSION}" ${PROJECT}

install:: .ensure.phony go.ensure
	cd pkg && GOBIN=$(PULUMI_BIN) go install -ldflags "-X github.com/pulumi/pulumi/pkg/v3/version.Version=${VERSION}" ${PROJECT}
else
build:: build_cover ensure_cover

ensure_cover::
	mkdir -p $(PULUMI_TEST_COVERAGE_PATH)

install:: install_cover
endif

build_debug::
	cd pkg && go install -gcflags="all=-N -l" -ldflags "-X github.com/pulumi/pulumi/pkg/v3/version.Version=${VERSION}" ${PROJECT}

build_cover::
	cd pkg && go test -coverpkg github.com/pulumi/pulumi/pkg/v3/...,github.com/pulumi/pulumi/sdk/v3/... -cover -c -o $(shell go env GOPATH)/bin/pulumi -ldflags "-X github.com/pulumi/pulumi/pkg/v3/version.Version=${VERSION}" ${PROJECT}

install_cover:: build_cover
	cp $(shell go env GOPATH)/bin/pulumi $(PULUMI_BIN)

developer_docs::
	cd developer-docs && make html

install_all:: install

dist:: build
	cd pkg && go install -ldflags "-X github.com/pulumi/pulumi/pkg/v3/version.Version=${VERSION}" ${PROJECT}

.PHONY: brew
# NOTE: the brew target intentionally avoids the dependency on `build`, as each language SDK has its own brew target
brew::
	./scripts/brew.sh "${PROJECT}"

.PHONY: lint_%
lint:: golangci-lint.ensure lint_pkg lint_sdk lint_tests
lint_pkg: lint_deps
	cd pkg && golangci-lint run -c ../.golangci.yml --timeout 5m
lint_sdk: lint_deps
	cd sdk && golangci-lint run -c ../.golangci.yml --timeout 5m
lint_tests: lint_deps
	cd tests && golangci-lint run -c ../.golangci.yml --timeout 5m
lint_deps:
	@echo "Check for golangci-lint"; [ -e "$(shell which golangci-lint)" ]
lint_actions:
	go run github.com/rhysd/actionlint/cmd/actionlint@v1.6.17 \
	  -format '{{range $$err := .}}### Error at line {{$$err.Line}}, col {{$$err.Column}} of `{{$$err.Filepath}}`\n\n{{$$err.Message}}\n\n```\n{{$$err.Snippet}}\n```\n\n{{end}}'

test_fast:: build get_schemas
	@cd pkg && $(GO_TEST_FAST) ${PROJECT_PKGS} ${PKG_CODEGEN_NODE}

test_all:: test_pkg test_integration

lang=$(subst test_codegen_,,$(word 1,$(subst !, ,$@)))
test_codegen_%: get_schemas
	@cd pkg && $(GO_TEST) ${PKG_CODEGEN}/${lang}/...

test_pkg_rest: get_schemas
	@cd pkg && $(GO_TEST) ${PROJECT_PKGS}

test_pkg:: test_pkg_rest test_codegen_dotnet test_codegen_go test_codegen_nodejs test_codegen_python

subset=$(subst test_integration_,,$(word 1,$(subst !, ,$@)))
test_integration_%:
	@cd tests && PULUMI_INTEGRATION_TESTS=$(subset) $(GO_TEST) $(INTEGRATION_PKG)

test_integration_subpkgs:
	@cd tests && $(GO_TEST) $(TESTS_PKGS)

test_integration:: $(SDKS:%=test_integration_%) test_integration_rest test_integration_subpkgs

# Used by CI to run tests in parallel across the Go modules pkg, sdk, and tests.
.PHONY: gotestsum/%
gotestsum/%:
	cd $* && $(PYTHON) '$(CURDIR)/scripts/go-test.py' $(GO_TEST_FLAGS) $${OPTS} $${PKGS}

tidy::
	./scripts/tidy.sh

validate_codecov_yaml::
	curl --data-binary @codecov.yml https://codecov.io/validate

# We replace the '!' with a space, then take the first word
# schema-pkg!x.y.z => schema-pkg
# We then replace 'schema-' with nothing, giving only the package name.
# schema-pkg => pkg
# Recall that `$@` is the target make is trying to build, in our case schema-pkg!x.y.z
name=$(subst schema-,,$(word 1,$(subst !, ,$@)))
# Here we take the second word, just the version
version=$(word 2,$(subst !, ,$@))
schema-%: curl.ensure jq.ensure
	@echo "Ensuring schema ${name}, ${version}"
	@# Download the package from github, then stamp in the correct version.
	@[ -f pkg/codegen/testing/test/testdata/${name}-${version}.json ] || \
		curl "https://raw.githubusercontent.com/pulumi/pulumi-${name}/v${version}/provider/cmd/pulumi-resource-${name}/schema.json" \
		| jq '.version = "${version}"' >  pkg/codegen/testing/test/testdata/${name}-${version}.json
	@# Confirm that the correct version is present. If not, error out.
	@FOUND="$$(jq -r '.version' pkg/codegen/testing/test/testdata/${name}-${version}.json)" &&        \
		if ! [ "$$FOUND" = "${version}" ]; then									           \
			echo "${name} required version ${version} but found existing version $$FOUND"; \
			exit 1;																		   \
		fi
# Related files:
#
# pkg/codegen/testing/utils/host.go depends on this list, update that file on changes.
#
# pkg/codegen/schema/schema_test.go depends on kubernetes@3.7.2, update that file on changes.
#
# As a courtesy to reviewers, please make changes to this list and the committed schema files in a
# separate commit from other changes, as online code review tools may balk at rendering these diffs.
get_schemas: \
			schema-aws!4.15.0           \
			schema-aws!4.26.0           \
			schema-aws!4.36.0           \
			schema-aws!4.37.1           \
			schema-aws!5.4.0            \
			schema-aws!5.16.2           \
			schema-azure-native!1.28.0  \
			schema-azure-native!1.29.0  \
			schema-azure-native!1.56.0  \
			schema-azure!4.18.0         \
			schema-kubernetes!3.0.0     \
			schema-kubernetes!3.7.0     \
			schema-kubernetes!3.7.2     \
			schema-random!4.2.0         \
			schema-random!4.3.1         \
			schema-eks!0.37.1           \
			schema-eks!0.40.0           \
			schema-docker!3.1.0         \
			schema-awsx!1.0.0-beta.5    \
			schema-aws-native!0.13.0    \
			schema-google-native!0.18.2

.PHONY: changelog
changelog:
	go run github.com/aaronfriel/go-change@v0.1.2 create
