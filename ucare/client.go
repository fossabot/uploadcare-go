package ucare

import (
	"context"
	"errors"
	"net/http"

	"github.com/uploadcare/uploadcare-go/internal/config"
)

// Client describes API client behaviour
type Client interface {
	NewRequest(
		ctx context.Context,
		endpoint config.Endpoint,
		method string,
		requrl string,
		data ReqEncoder,
	) (*http.Request, error)
	Do(req *http.Request, resdata interface{}) error
}

// APICreds holds per project API credentials.
// You can find your credentials on the uploadcare dashboard.
type APICreds struct {
	SecretKey string
	PublicKey string
}

// Config holds configuration for the client
type Config struct {
	// HTTPClient allowes you to set custom http client for the calls
	HTTPClient *http.Client
	// APIVersion specifies REST API version to be used
	APIVersion string
	// SignBasedAuthentication should be true if you want to use
	// signed uploads and signature based authentication for the
	// REST API calls.
	SignBasedAuthentication bool
}

// ReqEncoder exists to encode data into the prepared request.
// It may encode part of the data to the query string and other
// part into the request body. It may also set request headers for some
// payload types (multipart/form-data).
type ReqEncoder interface {
	EncodeReq(*http.Request) error
}

type client struct {
	backends map[config.Endpoint]Client
}

// NewClient initializes and configures new client for the high level API.
func NewClient(creds APICreds, conf *Config) (Client, error) {
	log.Infof("creating new client: %+v, %+v", creds, conf)

	if creds.SecretKey == "" || creds.PublicKey == "" {
		return nil, errors.New("uploadcare: invalid api creds provided")
	}

	conf = resolveConfig(conf)

	c := client{
		backends: map[config.Endpoint]Client{
			config.RESTAPIEndpoint:   newRESTAPIClient(creds, conf),
			config.UploadAPIEndpoint: newUploadAPIClient(creds, conf),
		},
	}

	return &c, nil
}

var errNoClient = errors.New("no client for such endpoint")

// NewRequests constructs new http request.
func (c *client) NewRequest(
	ctx context.Context,
	endpoint config.Endpoint,
	method string,
	requrl string,
	data ReqEncoder,
) (*http.Request, error) {
	b, ok := c.backends[endpoint]
	if !ok {
		return nil, errNoClient
	}

	return b.NewRequest(ctx, endpoint, method, requrl, data)
}

// Do performs the actual backend API call.
func (c *client) Do(req *http.Request, resdata interface{}) error {
	b, ok := c.backends[config.Endpoint(req.URL.Host)]
	if !ok {
		return errNoClient
	}
	return b.Do(req, resdata)
}

func resolveConfig(conf *Config) *Config {
	if conf == nil {
		conf = &Config{}
	}
	if conf.APIVersion == "" {
		conf.APIVersion = defaultAPIVersion
	}
	if conf.HTTPClient == nil {
		conf.HTTPClient = http.DefaultClient
	}
	return conf
}
