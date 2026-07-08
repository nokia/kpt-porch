// Copyright 2026 The kpt Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package httpclient

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func TestRegistryClientUsesHTTPTransport(t *testing.T) {
	client := RegistryClient(nil)
	require.NotNil(t, client)
	_, ok := client.Transport.(*http.Transport)
	assert.True(t, ok)
}

func TestRegistryClientAppliesTLSConfig(t *testing.T) {
	pool := x509.NewCertPool()
	tlsConfig := &tls.Config{
		RootCAs:    pool,
		MinVersion: tls.VersionTLS12,
	}

	client := RegistryClient(tlsConfig)
	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	assert.EqualValues(t, tls.VersionTLS12, transport.TLSClientConfig.MinVersion)
	assert.Equal(t, pool, transport.TLSClientConfig.RootCAs)
}

func TestRegistryClientWithWrappedDefaultTransport(t *testing.T) {
	oldDefaultTransport := http.DefaultTransport
	oldDefaultClientTransport := http.DefaultClient.Transport

	http.DefaultTransport = otelhttp.NewTransport(http.DefaultTransport)
	http.DefaultClient.Transport = http.DefaultTransport

	t.Cleanup(func() {
		http.DefaultTransport = oldDefaultTransport
		http.DefaultClient.Transport = oldDefaultClientTransport
	})

	assert.NotPanics(t, func() {
		client := RegistryClient(nil)
		_, ok := client.Transport.(*http.Transport)
		assert.True(t, ok)
	})
}
