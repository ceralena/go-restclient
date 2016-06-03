// Package restclient provides a simple client for talking to RESTful HTTP
// APIs that mostly return JSON responses.
//
//
// For handling response bodies, it supports either streams or automatically
// encoding & decoding of JSON.
package restclient

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

const (
	httpPortDefault  = 80
	httpsPortDefault = 443
)

// Client provides access to a HTTP service scoped to a particlar domain and
// root URL.
type Client interface {
	// Send a request with this method to this path.
	//
	// If the payload satisfies io.Reader, it will be streamed in the request body.
	//
	// Otherwise, it will be encoded as JSON. If JSON encoding fails, an error
	// is returned with no request sent.
	//
	// The response is assumed to be JSON and is parsed into the final argument.
	Do(method string, path string, payload, into interface{}) (int, error)

	// Similar behaviour to Do(), but the response is not assumed to be JSON.
	// Instead it's returned as a stream.
	//
	// The caller must call Close() on the response when it's finished with it.
	DoStream(method string, path string, payload interface{}) (int, io.ReadCloser, error)

	// Set an error constructor that will be used when processing any response
	// with a status code in the list of specified status codes.
	//
	// If this method is not used, 4xx and 5xx responses do not produce an error.
	//
	// This can be used to set a constructor that will be called if the status
	// code is in the specified set. The set can include any status code.
	//
	// The function is passed the Request and Response.
	//
	// Each call to SetErrorConstructor() overrides the effect of any previous
	// calls - it is not possible to set different handlers for different sets
	// of response status codes.
	SetErrorConstructor([]int, func(*http.Request, *http.Response) error)
}

// New creates a new HTTP client.
func New(host string, port int, useHTTPS bool) Client {
	return &httpClient{host, port, useHTTPS, nil, nil}
}

// HTTPClient implementation.
type httpClient struct {
	host     string
	port     int
	useHTTPS bool

	customErrorStatusCodes []int
	customErrorConstructor func(*http.Request, *http.Response) error
}

func (client *httpClient) fullPath(path string) string {
	var proto, port string
	var defaultPort int

	if client.useHTTPS {
		proto = "https"
		defaultPort = httpsPortDefault
	} else {
		proto = "http"
		defaultPort = httpPortDefault
	}

	if client.port != defaultPort {
		port = ":" + strconv.Itoa(client.port)
	}

	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return proto + "://" + client.host + port + path
}

func (client *httpClient) requestRaw(method string, path string, payload io.Reader) (int, io.ReadCloser, error) {
	fullPath := client.fullPath(path)

	var req *http.Request
	var err error

	req, err = http.NewRequest(method, fullPath, payload)

	if err != nil {
		return -1, nil, err
	}

	resp, err := (&http.Client{}).Do(req)

	if err != nil {
		return -1, nil, err
	} else if client.hasCustomError(resp.StatusCode) {
		return resp.StatusCode, nil, client.customErrorResponse(req, resp)
	}

	return resp.StatusCode, resp.Body, err
}

func (client *httpClient) request(method string, path string, payload interface{}) (int, io.ReadCloser, error) {
	rdr, ok := payload.(io.Reader)
	if !ok {
		// Payload is not a reader - assume it's JSON and try to encode it
		enc, err := json.Marshal(payload)
		if err != nil {
			return -1, nil, err
		}
		rdr = bytes.NewBuffer(enc)
	}
	return client.requestRaw(method, path, rdr)
}

func (client *httpClient) requestJSON(method, path string, payload interface{}, into interface{}) (int, error) {
	status, body, err := client.request(method, path, payload)

	if err != nil {
		return status, err
	}

	defer body.Close()

	return status, handleJSONResponse(body, into)
}

func handleJSONResponse(body io.Reader, into interface{}) error {
	if into == nil {
		return nil
	}
	dec := json.NewDecoder(body)
	return dec.Decode(into)
}

func (client *httpClient) Do(method, path string, payload interface{}, into interface{}) (int, error) {
	return client.requestJSON(method, path, payload, into)
}

func (client *httpClient) DoStream(method, path string, payload interface{}) (int, io.ReadCloser, error) {
	return client.request(method, path, payload)
}

func (client *httpClient) SetErrorConstructor(statusCodes []int, fn func(*http.Request, *http.Response) error) {
	client.customErrorStatusCodes = statusCodes
	client.customErrorConstructor = fn
}

func (client *httpClient) hasCustomError(statusCode int) bool {
	for _, c := range client.customErrorStatusCodes {
		if c == statusCode {
			return true
		}
	}
	return false
}

func (client *httpClient) customErrorResponse(req *http.Request, resp *http.Response) error {
	defer resp.Body.Close()
	return client.customErrorConstructor(req, resp)
}
