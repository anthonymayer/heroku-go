package heroku

import (
	"bytes"
	"code.google.com/p/go-uuid/uuid"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"runtime"
	"strings"
)

const (
	Version   = "0.1"
	userAgent = "heroku.go " + Version + " " + runtime.GOOS + " " + runtime.GOARCH
)

// A Client is a Heroku API client. Its zero value is a usable client that uses
// default settings for the Heroku API. The Client has an internal HTTP client
// (HTTP) which defaults to http.DefaultClient.
//
// As with all http.Clients, this Client's Transport has internal state (cached
// HTTP connections), so Clients should be reused instead of created as needed.
// Clients are safe for use by multiple goroutines.
type Client struct {
	// HTTP is the Client's internal http.Client, handling HTTP requests to the
	// Heroku API.
	HTTP *http.Client

	// The URL of the Heroku API to communicate with. Defaults to
	// "https://api.heroku.com".
	URL string

	// Username is the HTTP basic auth username for API calls made by this Client.
	Username string

	// Password is the HTTP basic auth password for API calls made by this Client.
	Password string
}

func (c *Client) Get(v interface{}, path string) error {
	return c.APIReq(v, "GET", path, nil)
}

func (c *Client) Patch(v interface{}, path string, body interface{}) error {
	return c.APIReq(v, "PATCH", path, body)
}

func (c *Client) Post(v interface{}, path string, body interface{}) error {
	return c.APIReq(v, "POST", path, body)
}

func (c *Client) Put(v interface{}, path string, body interface{}) error {
	return c.APIReq(v, "PUT", path, body)
}

func (c *Client) Delete(path string) error {
	return c.APIReq(nil, "DELETE", path, nil)
}

// Generates an HTTP request for the Heroku API, but does not
// perform the request. The request's Accept header field will be
// set to:
//
//   Accept: application/vnd.heroku+json; version=3
//
// The type of body determines how to encode the request:
//
//   nil         no body
//   io.Reader   body is sent verbatim
//   else        body is encoded as application/json
func (c *Client) NewRequest(method, path string, body interface{}) (*http.Request, error) {
	var ctype string
	var rbody io.Reader

	switch t := body.(type) {
	case nil:
	case io.Reader:
		rbody = t
	default:
		j, err := json.Marshal(body)
		if err != nil {
			log.Fatal(err)
		}
		rbody = bytes.NewReader(j)
		ctype = "application/json"
	}
	apiURL := strings.TrimRight(c.URL, "/")
	if apiURL == "" {
		apiURL = "https://api.heroku.com"
	}
	req, err := http.NewRequest(method, apiURL+path, rbody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.heroku+json; version=3")
	req.Header.Set("Request-Id", uuid.New())
	req.Header.Set("User-Agent", userAgent)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	req.SetBasicAuth(c.Username, c.Password)
	for _, h := range strings.Split(os.Getenv("HKHEADER"), "\n") {
		if i := strings.Index(h, ":"); i >= 0 {
			req.Header.Set(
				strings.TrimSpace(h[:i]),
				strings.TrimSpace(h[i+1:]),
			)
		}
	}
	return req, nil
}

// Sends a Heroku API request and decodes the response into v. As
// described in NewRequest(), the type of body determines how to
// encode the request body. As described in DoReq(), the type of
// v determines how to handle the response body.
func (c *Client) APIReq(v interface{}, meth, path string, body interface{}) error {
	req, err := c.NewRequest(meth, path, body)
	if err != nil {
		return err
	}
	return c.DoReq(req, v)
}

// Submits an HTTP request, checks its response, and deserializes
// the response into v. The type of v determines how to handle
// the response body:
//
//   nil        body is discarded
//   io.Writer  body is copied directly into v
//   else       body is decoded into v as json
//
func (c *Client) DoReq(req *http.Request, v interface{}) error {
	debug := os.Getenv("HKDEBUG") != ""
	if debug {
		dump, err := httputil.DumpRequestOut(req, true)
		if err != nil {
			log.Println(err)
		} else {
			os.Stderr.Write(dump)
			os.Stderr.Write([]byte{'\n', '\n'})
		}
	}

	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	res, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if debug {
		dump, err := httputil.DumpResponse(res, true)
		if err != nil {
			log.Println(err)
		} else {
			os.Stderr.Write(dump)
			os.Stderr.Write([]byte{'\n'})
		}
	}
	if err = checkResp(res); err != nil {
		return err
	}
	switch t := v.(type) {
	case nil:
	case io.Writer:
		_, err = io.Copy(t, res.Body)
	default:
		err = json.NewDecoder(res.Body).Decode(v)
	}
	return err
}

func checkResp(res *http.Response) error {
	if res.StatusCode == 401 {
		return errors.New("Unauthorized")
	}
	if res.StatusCode == 403 {
		return errors.New("Unauthorized")
	}
	if res.StatusCode/100 != 2 { // 200, 201, 202, etc
		return errors.New("Unexpected error: " + res.Status)
	}
	if msg := res.Header.Get("X-Heroku-Warning"); msg != "" {
		fmt.Fprintln(os.Stderr, strings.TrimSpace(msg))
	}
	return nil
}
