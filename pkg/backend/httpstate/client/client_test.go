// Copyright 2016-2021, Pulumi Corporation.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package client

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMockServer(statusCode int, message string) *httptest.Server {
	return httptest.NewServer(
		http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			rw.WriteHeader(statusCode)
			_, err := rw.Write([]byte(message))
			if err != nil {
				return
			}
		}))
}

func newMockServerRequestProcessor(statusCode int, processor func(req *http.Request) string) *httptest.Server {
	return httptest.NewServer(
		http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			rw.WriteHeader(statusCode)
			_, err := rw.Write([]byte(processor(req)))
			if err != nil {
				return
			}
		}))
}

func newMockClient(server *httptest.Server) *Client {
	return &Client{
		apiURL:   server.URL,
		apiToken: "",
		apiUser:  "",
		diag:     nil,
		client: &defaultRESTClient{
			client: &defaultHTTPClient{
				client: http.DefaultClient,
			},
		},
	}
}

func TestAPIErrorResponses(t *testing.T) {
	t.Parallel()

	t.Run("TestAuthError", func(t *testing.T) {
		t.Parallel()

		// check 401 error is handled
		unauthorizedServer := newMockServer(401, "401: Unauthorized")
		defer unauthorizedServer.Close()

		unauthorizedClient := newMockClient(unauthorizedServer)
		_, _, unauthorizedErr := unauthorizedClient.GetCLIVersionInfo(context.Background())

		assert.Error(t, unauthorizedErr)
		assert.Equal(t, unauthorizedErr.Error(), "this command requires logging in; try running `pulumi login` first")
	})
	t.Run("TestRateLimitError", func(t *testing.T) {
		t.Parallel()

		// test handling 429: Too Many Requests/rate-limit response
		rateLimitedServer := newMockServer(429, "rate-limit error")
		defer rateLimitedServer.Close()

		rateLimitedClient := newMockClient(rateLimitedServer)
		_, _, rateLimitErr := rateLimitedClient.GetCLIVersionInfo(context.Background())

		assert.Error(t, rateLimitErr)
		assert.Equal(t, rateLimitErr.Error(), "pulumi service: request rate-limit exceeded")
	})
	t.Run("TestDefaultError", func(t *testing.T) {
		t.Parallel()

		// test handling non-standard error message
		defaultErrorServer := newMockServer(418, "I'm a teapot")
		defer defaultErrorServer.Close()

		defaultErrorClient := newMockClient(defaultErrorServer)
		_, _, defaultErrorErr := defaultErrorClient.GetCLIVersionInfo(context.Background())

		assert.Error(t, defaultErrorErr)
	})
}

func TestGzip(t *testing.T) {
	t.Parallel()

	// test handling non-standard error message
	gzipCheckServer := newMockServerRequestProcessor(200, func(req *http.Request) string {
		assert.Equal(t, req.Header.Get("Content-Encoding"), "gzip")
		return "{}"
	})
	defer gzipCheckServer.Close()
	client := newMockClient(gzipCheckServer)

	// POST /import
	_, err := client.ImportStackDeployment(context.Background(), StackIdentifier{}, nil)
	assert.NoError(t, err)

	// PATCH /checkpoint
	err = client.PatchUpdateCheckpoint(context.Background(), UpdateIdentifier{}, nil, "")
	assert.NoError(t, err)

	// POST /events/batch
	err = client.RecordEngineEvents(context.Background(), UpdateIdentifier{}, apitype.EngineEventBatch{}, "")
	assert.NoError(t, err)

	// POST /events/batch
	_, err = client.BulkDecryptValue(context.Background(), StackIdentifier{}, nil)
	assert.NoError(t, err)

}

func TestPatchUpdateCheckpointVerbatimPreservesIndent(t *testing.T) {
	t.Parallel()

	deployment := apitype.DeploymentV3{
		Resources: []apitype.ResourceV3{{URN: resource.URN("urn1")}},
	}

	var indented json.RawMessage
	{
		indented1, err := json.MarshalIndent(deployment, "", "")
		require.NoError(t, err)
		untyped := apitype.UntypedDeployment{
			Version:    3,
			Deployment: indented1,
		}
		indented2, err := json.MarshalIndent(untyped, "", "")
		require.NoError(t, err)
		indented = indented2
	}

	var request apitype.PatchUpdateVerbatimCheckpointRequest

	server := newMockServerRequestProcessor(200, func(req *http.Request) string {

		reader, err := gzip.NewReader(req.Body)
		assert.NoError(t, err)
		defer reader.Close()

		err = json.NewDecoder(reader).Decode(&request)
		assert.NoError(t, err)

		return "{}"
	})

	client := newMockClient(server)

	sequenceNumber := 1

	err := client.PatchUpdateCheckpointVerbatim(context.Background(),
		UpdateIdentifier{}, sequenceNumber, indented, "token")
	assert.NoError(t, err)

	assert.Equal(t, string(indented), string(request.UntypedDeployment))
}
