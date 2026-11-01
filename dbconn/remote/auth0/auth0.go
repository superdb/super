package auth0

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/superdb/super/service/api"
)

type Tokens struct {
	Access     string    `json:"access"`
	Expiration time.Time `json:"expiration"`
	ID         string    `json:"id"`
	Refresh    string    `json:"refresh"`
}

type APIError struct {
	Kind             string `json:"error"` // renamed to avoid Error() clash
	ErrorDescription string `json:"error_description"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("auth0 error: %s: %s", e.Kind, e.ErrorDescription)
}

type DeviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
}

type tokenRequest struct {
	ClientID     string `json:"client_id"`
	DeviceCode   string `json:"device_code,omitempty"`
	GrantType    string `json:"grant_type"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
}

type Client struct {
	config api.AuthMethodAuth0Details
	url    url.URL
}

func NewClient(config api.AuthMethodAuth0Details) (*Client, error) {
	u, err := url.Parse(config.Domain)
	if err != nil {
		return nil, err
	}
	return &Client{config: config, url: *u}, nil
}

func (c *Client) GetDeviceCode(ctx context.Context, scope string) (DeviceCodeResponse, error) {
	type deviceCodeRequest struct {
		Audience string `json:"audience"`
		ClientID string `json:"client_id"`
		Scope    string `json:"scope"`
	}
	var res DeviceCodeResponse
	err := c.post(ctx, "/oauth/device/code", deviceCodeRequest{
		Audience: c.config.Audience,
		ClientID: c.config.ClientID,
		Scope:    scope,
	}, &res)
	return res, err
}

func (c *Client) PollDeviceCodeTokens(ctx context.Context, dcr DeviceCodeResponse) (Tokens, error) {
	delay := time.Duration(dcr.Interval) * time.Second
	if delay <= 0 {
		delay = time.Second
	}
	for {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return Tokens{}, ctx.Err()
		}
		tokens, err := c.getDeviceCodeTokens(ctx, dcr)
		if err != nil {
			var aerr *APIError
			if errors.As(err, &aerr) && aerr.Kind == "authorization_pending" {
				continue
			}
		}
		return tokens, err
	}
}

func (c *Client) getDeviceCodeTokens(ctx context.Context, dcr DeviceCodeResponse) (Tokens, error) {
	var res tokenResponse
	err := c.post(ctx, "/oauth/token", tokenRequest{
		ClientID:   c.config.ClientID,
		DeviceCode: dcr.DeviceCode,
		GrantType:  "urn:ietf:params:oauth:grant-type:device_code",
	}, &res)
	if err != nil {
		return Tokens{}, err
	}
	return Tokens{
		Access:     res.AccessToken,
		Expiration: time.Now().Add(time.Duration(res.ExpiresIn) * time.Second),
		ID:         res.IDToken,
		Refresh:    res.RefreshToken,
	}, nil
}

func (c *Client) RefreshToken(ctx context.Context, refreshToken string) (Tokens, error) {
	var res tokenResponse
	err := c.post(ctx, "/oauth/token", tokenRequest{
		ClientID:     c.config.ClientID,
		GrantType:    "refresh_token",
		RefreshToken: refreshToken,
	}, &res)
	if err != nil {
		return Tokens{}, err
	}
	if res.RefreshToken != "" {
		// res.RefreshToken is only set when refresh token rotation is
		// enabled for the Auth0 application specified by c.config.ClientID.
		refreshToken = res.RefreshToken
	}
	return Tokens{
		Access:     res.AccessToken,
		Expiration: time.Now().Add(time.Duration(res.ExpiresIn) * time.Second),
		ID:         res.IDToken,
		Refresh:    refreshToken,
	}, nil
}

func (c *Client) post(ctx context.Context, path string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	u := c.url
	u.Path = path
	req, err := http.NewRequestWithContext(ctx, "POST", u.String(), bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return parseError(res.Body)
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func parseError(body io.Reader) error {
	var aerr APIError
	if err := json.NewDecoder(body).Decode(&aerr); err != nil {
		return err
	}
	return &aerr
}
