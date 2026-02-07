package testutil

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// NewJSONRequest creates an *http.Request with JSON body and Content-Type header.
func NewJSONRequest(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		err := json.NewEncoder(&buf).Encode(body)
		require.NoError(t, err, "failed to encode request body")
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	return req
}

// WithSecretHeaders adds X-Slurpee-Secret-ID and X-Slurpee-Secret headers to a request.
func WithSecretHeaders(req *http.Request, secretID, secretValue string) *http.Request {
	req.Header.Set("X-Slurpee-Secret-ID", secretID)
	req.Header.Set("X-Slurpee-Secret", secretValue)
	return req
}

// WithAdminSecret adds the X-Slurpee-Admin-Secret header to a request.
func WithAdminSecret(req *http.Request, adminSecret string) *http.Request {
	req.Header.Set("X-Slurpee-Admin-Secret", adminSecret)
	return req
}

// AssertJSONResponse reads the response body as JSON, asserts the status code,
// and unmarshals into the provided target. Returns the raw body bytes.
func AssertJSONResponse(t *testing.T, rec *httptest.ResponseRecorder, expectedStatus int, target any) []byte {
	t.Helper()
	assert.Equal(t, expectedStatus, rec.Code, "unexpected status code")
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")

	body := rec.Body.Bytes()
	if target != nil {
		err := json.Unmarshal(body, target)
		require.NoError(t, err, "failed to unmarshal response body: %s", string(body))
	}
	return body
}

// AssertJSONError asserts that the response has the expected status code and
// contains an "error" field with the expected message substring.
func AssertJSONError(t *testing.T, rec *httptest.ResponseRecorder, expectedStatus int, errorSubstring string) {
	t.Helper()
	var resp map[string]string
	AssertJSONResponse(t, rec, expectedStatus, &resp)
	assert.Contains(t, resp["error"], errorSubstring, "error message should contain expected substring")
}
