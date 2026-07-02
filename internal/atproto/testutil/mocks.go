//go:build test

// Package atprototestutil provides test doubles for internal/atproto,
// built only against its public API (HTTPDoer) so it can be imported from
// both internal/atproto's own tests and any future consumer's tests.
package atprototestutil

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sync"
)

// RecordedRequest captures the parts of an *http.Request a test needs to
// assert on, without holding a reference to the original request (whose
// body would already be consumed).
type RecordedRequest struct {
	Method string
	URL    string
	Body   []byte
}

// MockHTTPDoer is a lightweight atproto.HTTPDoer implementation that
// records every request it receives and delegates response generation to
// a caller-supplied Handler, so different tests can script different
// sequences of responses (DID resolution, login, pagination, deletion,
// etc.) without any real network I/O.
type MockHTTPDoer struct {
	mu       sync.Mutex
	requests []RecordedRequest

	// Handler produces the response (or error) for each request. Tests
	// set this to inspect req.URL/req.Method and return the appropriate
	// canned *http.Response.
	Handler func(req *http.Request) (*http.Response, error)
}

// Do implements atproto.HTTPDoer.
func (m *MockHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		b, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("MockHTTPDoer: read request body: %w", err)
		}
		body = b
		_ = req.Body.Close()
		req.Body = io.NopCloser(bytes.NewReader(b))
	}

	m.mu.Lock()
	m.requests = append(m.requests, RecordedRequest{Method: req.Method, URL: req.URL.String(), Body: body})
	m.mu.Unlock()

	if m.Handler == nil {
		return nil, fmt.Errorf("MockHTTPDoer: no handler configured for %s %s", req.Method, req.URL)
	}
	return m.Handler(req)
}

// Requests returns a copy of the requests recorded so far, in call order.
func (m *MockHTTPDoer) Requests() []RecordedRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.requests)
}

// CallCount returns the number of requests recorded so far.
func (m *MockHTTPDoer) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.requests)
}

// JSONResponse builds an *http.Response with the given status code, writing
// body verbatim (the caller is responsible for supplying valid JSON) and
// setting a JSON Content-Type header, for use inside a MockHTTPDoer.Handler.
func JSONResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(bytes.NewReader([]byte(body))),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}
