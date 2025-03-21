// Copyright 2016-2021, Pulumi Corporation.  All rights reserved.
//go:build go || all

package ints

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pulumi/pulumi/pkg/v3/testing/integration"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
)

// TestEmptyGo simply tests that we can build and run an empty Go project.
func TestEmptyGo(t *testing.T) {
	integration.ProgramTest(t, &integration.ProgramTestOptions{
		Dir: filepath.Join("empty", "go"),
		Dependencies: []string{
			"github.com/pulumi/pulumi/sdk/v3",
		},
		Quick: true,
	})
}

// Tests that stack references work in Go.
func TestStackReferenceGo(t *testing.T) {
	if owner := os.Getenv("PULUMI_TEST_OWNER"); owner == "" {
		t.Skipf("Skipping: PULUMI_TEST_OWNER is not set")
	}

	opts := &integration.ProgramTestOptions{
		RequireService: true,

		Dir: filepath.Join("stack_reference", "go"),
		Dependencies: []string{
			"github.com/pulumi/pulumi/sdk/v3",
		},
		Quick: true,
		EditDirs: []integration.EditDir{
			{
				Dir:      "step1",
				Additive: true,
			},
			{
				Dir:      "step2",
				Additive: true,
			},
		},
	}
	integration.ProgramTest(t, opts)
}

// Test remote component construction in Go.
func TestConstructGo(t *testing.T) {
	t.Parallel()

	testDir := "construct_component"
	runComponentSetup(t, testDir)

	tests := []struct {
		componentDir          string
		expectedResourceCount int
		env                   []string
	}{
		{
			componentDir:          "testcomponent",
			expectedResourceCount: 9,
			// TODO[pulumi/pulumi#5455]: Dynamic providers fail to load when used from multi-lang components.
			// Until we've addressed this, set PULUMI_TEST_YARN_LINK_PULUMI, which tells the integration test
			// module to run `yarn install && yarn link @pulumi/pulumi` in the Go program's directory, allowing
			// the Node.js dynamic provider plugin to load.
			// When the underlying issue has been fixed, the use of this environment variable inside the integration
			// test module should be removed.
			env: []string{"PULUMI_TEST_YARN_LINK_PULUMI=true"},
		},
		{
			componentDir:          "testcomponent-python",
			expectedResourceCount: 9,
		},
		{
			componentDir:          "testcomponent-go",
			expectedResourceCount: 8, // One less because no dynamic provider.
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.componentDir, func(t *testing.T) {
			localProviders :=
				[]integration.LocalDependency{
					{Package: "testprovider", Path: buildTestProvider(t, filepath.Join("..", "testprovider"))},
					{Package: "testcomponent", Path: filepath.Join(testDir, test.componentDir)},
				}
			integration.ProgramTest(t, optsForConstructGo(t, testDir, test.expectedResourceCount, localProviders, test.env...))
		})
	}
}

// Test remote component construction in Go.
func TestNestedConstructGo(t *testing.T) {
	testDir := "construct_component"
	runComponentSetup(t, testDir)

	localProviders :=
		[]integration.LocalDependency{
			{Package: "testprovider", Path: buildTestProvider(t, filepath.Join("..", "testprovider"))},
			{Package: "testcomponent", Path: filepath.Join(testDir, "testcomponent-go")},
			{Package: "secondtestcomponent", Path: filepath.Join(testDir, "testcomponent2-go")}}
	integration.ProgramTest(t, optsForConstructGo(t, "construct_nested_component", 18, localProviders))
}

func optsForConstructGo(t *testing.T, dir string, expectedResourceCount int, localProviders []integration.LocalDependency, env ...string) *integration.ProgramTestOptions {
	return &integration.ProgramTestOptions{
		Env: env,
		Dir: filepath.Join(dir, "go"),
		Dependencies: []string{
			"github.com/pulumi/pulumi/sdk/v3",
		},
		LocalProviders: localProviders,
		Secrets: map[string]string{
			"secret": "this super secret is encrypted",
		},
		Quick: true,
		ExtraRuntimeValidation: func(t *testing.T, stackInfo integration.RuntimeValidationStackInfo) {
			assert.NotNil(t, stackInfo.Deployment)
			if assert.Equal(t, expectedResourceCount, len(stackInfo.Deployment.Resources)) {
				stackRes := stackInfo.Deployment.Resources[0]
				assert.NotNil(t, stackRes)
				assert.Equal(t, resource.RootStackType, stackRes.Type)
				assert.Equal(t, "", string(stackRes.Parent))

				// Check that dependencies flow correctly between the originating program and the remote component
				// plugin.
				urns := make(map[string]resource.URN)
				for _, res := range stackInfo.Deployment.Resources[1:] {
					assert.NotNil(t, res)

					urns[string(res.URN.Name())] = res.URN
					switch res.URN.Name() {
					case "child-a":
						for _, deps := range res.PropertyDependencies {
							assert.Empty(t, deps)
						}
					case "child-b":
						expected := []resource.URN{urns["a"]}
						assert.ElementsMatch(t, expected, res.Dependencies)
						assert.ElementsMatch(t, expected, res.PropertyDependencies["echo"])
					case "child-c":
						expected := []resource.URN{urns["a"], urns["child-a"]}
						assert.ElementsMatch(t, expected, res.Dependencies)
						assert.ElementsMatch(t, expected, res.PropertyDependencies["echo"])
					case "a", "b", "c":
						secretPropValue, ok := res.Outputs["secret"].(map[string]interface{})
						assert.Truef(t, ok, "secret output was not serialized as a secret")
						assert.Equal(t, resource.SecretSig, secretPropValue[resource.SigKey].(string))
					}
				}
			}
		},
	}
}

// TestRefreshGo simply tests that we can build and run an empty Go project with the `refresh` option set.
func TestRefreshGo(t *testing.T) {
	integration.ProgramTest(t, &integration.ProgramTestOptions{
		Dir: filepath.Join("refresh", "go"),
		Dependencies: []string{
			"github.com/pulumi/pulumi/sdk/v3",
		},
		Quick: true,
	})
}
