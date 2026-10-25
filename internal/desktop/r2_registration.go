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
	"sync"
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

type r2ListenAddress struct {
	network, address string
	// Optional addresses may be unavailable, such as IPv6 loopback on a host
	// without IPv6; the callback then listens on the required address only.
	optional bool
}

// A localhost callback may resolve to either loopback family, so both are
// served. Literal IP callbacks listen only on that address.
func r2CallbackListenAddresses(u *url.URL) []r2ListenAddress {
	host, port := u.Hostname(), u.Port()
	if host == "localhost" {
		return []r2ListenAddress{{network: "tcp4", address: net.JoinHostPort("127.0.0.1", port)}, {network: "tcp6", address: net.JoinHostPort("::1", port), optional: true}}
	}
	network := "tcp4"
	if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
		network = "tcp6"
	}
	return []r2ListenAddress{{network: network, address: net.JoinHostPort(host, port)}}
}

func listenR2Callback(u *url.URL) (net.Listener, error) {
	var listeners []net.Listener
	for _, candidate := range r2CallbackListenAddresses(u) {
		listener, err := net.Listen(candidate.network, candidate.address)
		if err != nil {
			if candidate.optional {
				continue
			}
			for _, l := range listeners {
				_ = l.Close()
			}
			return nil, err
		}
		listeners = append(listeners, listener)
	}
	if len(listeners) == 1 {
		return listeners[0], nil
	}
	return newMultiListener(listeners), nil
}

// multiListener accepts connections from several listeners as one, so a single
// http.Server and a single Close cover every loopback family.
type multiListener struct {
	listeners []net.Listener
	conns     chan net.Conn
	done      chan struct{}
	once      sync.Once
}

func newMultiListener(listeners []net.Listener) *multiListener {
	m := &multiListener{listeners: listeners, conns: make(chan net.Conn), done: make(chan struct{})}
	for _, listener := range listeners {
		go func() {
			for {
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				select {
				case m.conns <- conn:
				case <-m.done:
					_ = conn.Close()
					return
				}
			}
		}()
	}
	return m
}

func (m *multiListener) Accept() (net.Conn, error) {
	select {
	case conn := <-m.conns:
		return conn, nil
	case <-m.done:
		return nil, net.ErrClosed
	}
}

func (m *multiListener) Close() error {
	m.once.Do(func() {
		close(m.done)
		for _, listener := range m.listeners {
			_ = listener.Close()
		}
	})
	return nil
}

func (m *multiListener) Addr() net.Addr {
	return m.listeners[0].Addr()
}
