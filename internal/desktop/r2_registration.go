package desktop

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// The distributor fills only this public application registration. User
// credentials are always obtained through OAuth and kept in the system store.
//
//go:embed cloudflare-registration.json
var cloudflareRegistration []byte

// Set through -X for build tooling that supplies registration separately.
var cloudflareOAuthRedirectURL string

type r2OAuthRegistration struct {
	ClientID    string
	RedirectURL string
}

var errR2Registration = errors.New("Cloudflare OAuth registration is invalid. Check cloudflare-registration.json.")

func loadR2Registration(data []byte, fallbackClientID, buildClientID, buildRedirectURL string, getenv func(string) string) (r2OAuthRegistration, error) {
	var config r2OAuthRegistration
	if trimmed := bytes.TrimSpace(data); len(trimmed) == 0 || trimmed[0] != '{' {
		return config, errR2Registration
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&config) != nil || decoder.Decode(new(any)) != io.EOF {
		return config, errR2Registration
	}
	if buildClientID != "" {
		config.ClientID = buildClientID
	}
	if buildRedirectURL != "" {
		config.RedirectURL = buildRedirectURL
	}
	if value := strings.TrimSpace(getenv("RACOMI_CLOUDFLARE_CLIENT_ID")); value != "" {
		config.ClientID = value
	}
	if value := getenv("RACOMI_CLOUDFLARE_REDIRECT_URL"); value != "" {
		config.RedirectURL = value
	}
	config.ClientID = strings.TrimSpace(config.ClientID)
	if config.ClientID == "" {
		config.ClientID = fallbackClientID
	}
	if len(config.ClientID) > 256 || strings.ContainsAny(config.ClientID, "\x00\r\n\t ") {
		return config, errR2Registration
	}
	if config.RedirectURL == "" {
		config.RedirectURL = r2OAuthRedirectURI
	}
	if _, err := r2CallbackURL(config.RedirectURL); err != nil {
		return config, errR2Registration
	}
	return config, nil
}

// Native authorization codes may only return to an explicit local HTTP
// listener. In particular, a registration typo must never send codes off-device.
func r2CallbackURL(value string) (*url.URL, error) {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "http" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(value, "#") || u.Path != "/oauth/cloudflare/callback" || u.RawPath != "" || u.Opaque != "" {
		return nil, errR2Registration
	}
	host := u.Hostname()
	loopback := host == "localhost"
	if ip := net.ParseIP(host); ip != nil {
		loopback = ip.IsLoopback()
	}
	port, portErr := strconv.Atoi(u.Port())
	if !loopback || portErr != nil || port < 1 || port > 65535 {
		return nil, errR2Registration
	}
	return u, nil
}

func r2CallbackListenAddress(u *url.URL) (string, string) {
	host, network := u.Hostname(), "tcp4"
	if host == "localhost" {
		host = "127.0.0.1"
	}
	if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
		network = "tcp6"
	}
	return network, net.JoinHostPort(host, u.Port())
}
