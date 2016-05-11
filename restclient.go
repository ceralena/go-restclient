// Package httpclient provides a simple client for talking to RESTful HTTP
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
	http_port_default  = 80
	https_port_default = 443
)

// A HTTPClient provides access to a HTTP service scoped to a particlar domain and root URL.
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

// Create a new HTTP client.
func New(host string, port int, use_https bool) Client {
	return &httpClient{host, port, use_https, nil, nil}
}

// HTTPClient implementation.
type httpClient struct {
	host      string
	port      int
	use_https bool

	customErrorStatusCodes []int
	customErrorConstructor func(*http.Request, *http.Response) error
}

func (client *httpClient) fullPath(path string) string {
	var proto, port string
	var default_port int

	if client.use_https {
		proto = "https"
		default_port = https_port_default
	} else {
		proto = "http"
		default_port = http_port_default
	}

	if client.port != default_port {
		port = ":" + strconv.Itoa(client.port)
	}

	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return proto + "://" + client.host + port + path
}

func (client *httpClient) requestRaw(path string, method string, payload io.Reader) (int, io.ReadCloser, error) {
	full_path := client.fullPath(path)

	var req *http.Request
	var err error

	req, err = http.NewRequest(method, full_path, payload)

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

func (client *httpClient) request(path string, method string, payload interface{}) (int, io.ReadCloser, error) {
	rdr, ok := payload.(io.Reader)
	if !ok {
		// Payload is not a reader - assume it's JSON and try to encode it
		enc, err := json.Marshal(payload)
		if err != nil {
			return -1, nil, err
		}
		rdr = bytes.NewBuffer(enc)
	}
	return client.requestRaw(path, method, rdr)
}

func (client *httpClient) requestJSON(path string, method string, payload interface{}, into interface{}) (int, error) {
	status, body, err := client.request(path, method, payload)

	if err != nil {
		return status, err
	}

	defer body.Close()
	dec := json.NewDecoder(body)
	return status, dec.Decode(into)
}

func (client *httpClient) Do(path, method string, payload interface{}, into interface{}) (int, error) {
	return client.requestJSON(path, method, payload, into)
}

func (client *httpClient) DoStream(path, method string, payload interface{}) (int, io.ReadCloser, error) {
	return client.request(path, method, payload)
}

func (client *httpClient) SetErrorConstructor(status_codes []int, fn func(*http.Request, *http.Response) error) {
	client.customErrorStatusCodes = status_codes
	client.customErrorConstructor = fn
}

func (client *httpClient) hasCustomError(status_code int) bool {
	for _, c := range client.customErrorStatusCodes {
		if c == status_code {
			return true
		}
	}
	return false
}

func (client *httpClient) customErrorResponse(req *http.Request, resp *http.Response) error {
	return client.customErrorConstructor(req, resp)
}
