package restclient

import (
	"testing"

	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
)

func TestHttpClientPath(t *testing.T) {
	cases := []struct {
		client Client
		path   string
		expect string
	}{
		{New("foo.net", 80, false), "/haha", "http://foo.net/haha"},
		{New("bar.net", 81, false), "/haha", "http://bar.net:81/haha"},

		{New("bang.net", 443, true), "/haha", "https://bang.net/haha"},
		{New("bux.net", 444, true), "/haha", "https://bux.net:444/haha"},

		// Make sure it works without the forward slash prefix
		{New("foo.net", 80, false), "haha", "http://foo.net/haha"},
		{New("bar.net", 81, false), "haha", "http://bar.net:81/haha"},
	}

	for _, c := range cases {
		p := c.client.(*httpClient).fullPath(c.path)
		if p != c.expect {
			t.Errorf("Bad full path for %q: got %q, expected %q", c.path, p, c.expect)
			t.Fail()
		}
	}
}

func TestHttpClientRequests(t *testing.T) {
	type testcase struct {
		endpoint        string
		expect_status   int
		expect_request  *testRequest
		expect_response testResponse
		expect_method   string
	}

	cases := []testcase{
		// GET requests
		{"/hello", 200, nil, testResponse{"something"}, "GET"},
		{"/hi", 200, nil, testResponse{"friendship"}, "GET"},

		// POST requests with and without body
		{"/aha", 200, &testRequest{"my cool tracks"}, testResponse{"yes well i"}, "POST"},
		{"/ahaaaa", 200, nil, testResponse{"ah the ah yeah"}, "POST"},

		// PUT requests with and without body
		{"/welb", 200, &testRequest{"well it's me"}, testResponse{"ahh this could truly be the"}, "PUT"},
		{"/welbababa", 200, nil, testResponse{"look i just don't want"}, "PUT"},

		// DELETE request
		{"/krenkt", 200, nil, testResponse{"the one and"}, "DELETE"},

		//
		// Requests that should fail.
		//
		{"/some_bad", 400, nil, testResponse{}, "GET"},
	}

	// First loop over the cases once creating all the handlers
	handlers := make(map[string]http.HandlerFunc)
	for _, c := range cases {
		handlers[c.endpoint] = makeTestHandler(t, c.endpoint, c.expect_method, c.expect_request, c.expect_status, c.expect_response)
	}
	server, err := startTestServer(handlers)
	if err != nil {
		t.Errorf("Could not start test server: %s", err)
		t.FailNow()
	}
	defer server.Stop()

	// Now we can create our client and start making requests
	client := New("localhost", server.port, false)
	for _, c := range cases {
		for i := 0; i < 2; i++ {

			var res testResponse
			var status int
			var err error
			var rdr io.ReadCloser

			switch i {
			case 0:
				status, err = client.Do(c.expect_method, c.endpoint, c.expect_request, &res)
			default:
				b, err := json.Marshal(c.expect_request)
				if err != nil {
					t.Errorf("Failed to marshal request: %s", err)
					t.Fail()
					continue
				}
				buf := bytes.NewBuffer(b)
				status, rdr, err = client.DoStream(c.expect_method, c.endpoint, buf)
			}

			if rdr != nil {
				dec := json.NewDecoder(rdr)
				err := dec.Decode(&res)
				if err != nil {
					t.Errorf("Failed to parse response: %s", err)
					t.Fail()
					continue
				}
			}

			if err != nil {
				t.Errorf("Error in request: %s", err)
				t.Fail()
			} else if status != c.expect_status {
				t.Errorf("Did not get status %d for %s %s", c.expect_status, c.expect_method, c.endpoint)
				t.Fail()
			} else if res.Response != c.expect_response.Response {
				t.Errorf("Did not get expected response: wanted %#v, got %#v", c.expect_response, res)
			}
		}
	}

}

type testResponse struct {
	Response string `json:"response"`
}

type testRequest struct {
	Query string
}

type testServer struct {
	l    net.Listener
	port int
}

func (t *testServer) Stop() {
	t.l.Close()
}

func startTestServer(handlers map[string]http.HandlerFunc) (*testServer, error) {
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{
		IP: net.ParseIP("127.0.0.1"), Port: 0,
	})
	if err != nil {
		return nil, err
	}

	port := listener.Addr().(*net.TCPAddr).Port

	ready := make(chan struct{})
	go func() {
		// Create a http server that will handle the cases we're going to test
		// for. There's a handler function created for each endpoint; each of
		// them only supports a particular method.
		mux := http.NewServeMux()
		for p, h := range handlers {
			mux.HandleFunc(p, h)
		}
		close(ready)
		http.Serve(listener, mux)
	}()

	<-ready

	return &testServer{listener, port}, nil
}

func makeTestHandler(t *testing.T, endpoint, expect_method string, expect_request *testRequest, expect_status int, expect_response testResponse) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != expect_method {
			t.Errorf("Got method %s for %s, expected %s", r.Method, endpoint, expect_method)
			t.Fail()
		}
		if expect_request != nil {
			var req testRequest
			dec := json.NewDecoder(r.Body)
			err := dec.Decode(&req)
			if err != nil {
				t.Errorf("Failed to decode HTTP request: %s", err)
				t.Fail()
				w.WriteHeader(500)
				enc := json.NewEncoder(w)
				enc.Encode(map[string]string{"error": fmt.Sprintf("Error decoding http request: %s", err)})
				return
			}
		}

		w.WriteHeader(expect_status)

		// Send the expected error response
		enc := json.NewEncoder(w)
		err := enc.Encode(expect_response)
		if err != nil {
			t.Errorf("Failed to marshal JSON response: %s", err)
			t.FailNow()
		}
	}
}

func TestCustomErrorConstructor(t *testing.T) {
	type constructorTestCase struct {
		endpoint            string
		expect_response     testResponse
		custom_err_statuses []int
		custom_err_handler  func(*http.Request, *http.Response) error
		expect_status       int
		expect_method       string
		expect_error        string
	}
	cases := []constructorTestCase{
		{"/some_cons_bad", testResponse{"hi my friends"}, []int{400}, func(_ *http.Request, resp *http.Response) error {
			var res testResponse
			dec := json.NewDecoder(resp.Body)
			err := dec.Decode(&res)
			if err != nil {
				return err
			}
			return fmt.Errorf("response '%s' is not valid mate.", res.Response)
		}, 400, "POST", "response 'hi my friends' is not valid mate."},
	}

	// First loop over the cases once creating all the handlers
	handlers := make(map[string]http.HandlerFunc)
	for _, c := range cases {
		handlers[c.endpoint] = makeTestHandler(t, c.endpoint, c.expect_method, nil, c.expect_status, c.expect_response)
	}
	server, err := startTestServer(handlers)
	if err != nil {
		t.Errorf("Could not start test server: %s", err)
		t.FailNow()
	}
	defer server.Stop()

	// Now we can create our client and start making requests
	client := New("localhost", server.port, false)
	for _, c := range cases {
		client.SetErrorConstructor(c.custom_err_statuses, c.custom_err_handler)

		var res testResponse

		status, err := client.Do(c.expect_method, c.endpoint, nil, &res)

		if err == nil && c.expect_error != "" {
			fmt.Println("hi sir")
			t.Fail()
		} else if err != nil && c.expect_error != err.Error() {
			fmt.Println("hi sir")
			t.Fail()
		} else if err != nil && c.expect_error == "" {
			t.Errorf("Error in request: %s", err)
			t.Fail()
		} else if status != c.expect_status {
			t.Errorf("Did not get status %d for %s %s", c.expect_status, c.expect_method, c.endpoint)
			t.Fail()
		}
	}
}

func TestStreamRequest(t *testing.T) {

}
