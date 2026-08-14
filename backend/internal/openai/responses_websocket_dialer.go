package openai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

type coderResponsesWebSocketDialer struct {
	readLimit        int64
	outboundProxyURL string
	proxyMu          sync.Mutex
	proxyClients     map[string]*responsesWebSocketProxyClientEntry
	closed           bool
}

type responsesWebSocketProxyClientEntry struct {
	client   *http.Client
	lastUsed time.Time
}

func (d *coderResponsesWebSocketDialer) Dial(ctx context.Context, target string, headers http.Header, proxyURL string) (ResponsesWebSocketConn, int, http.Header, error) {
	options := &websocket.DialOptions{HTTPHeader: headers.Clone(), CompressionMode: websocket.CompressionContextTakeover}
	httpClient, err := d.proxyHTTPClient(proxyURL)
	if err != nil {
		return nil, 0, nil, err
	}
	if httpClient != nil {
		options.HTTPClient = httpClient
	}
	connection, response, err := websocket.Dial(ctx, target, options)
	status := 0
	responseHeaders := responseHeader(response)
	if response != nil {
		status = response.StatusCode
	}
	if err != nil {
		var responseBody []byte
		if response != nil && response.Body != nil {
			responseBody, _ = io.ReadAll(io.LimitReader(response.Body, 8<<10))
			_ = response.Body.Close()
		}
		return nil, status, responseHeaders, &ResponsesWebSocketDialError{
			StatusCode: status, ResponseHeaders: responseHeaders, ResponseBody: responseBody, Err: err,
		}
	}
	connection.SetReadLimit(positiveInt64(d.readLimit, defaultWSUpstreamLimit))
	return connection, status, responseHeaders, nil
}

func (d *coderResponsesWebSocketDialer) proxyHTTPClient(accountProxyURL string) (*http.Client, error) {
	proxyURL := strings.TrimSpace(accountProxyURL)
	if proxyURL == "" {
		proxyURL = strings.TrimSpace(d.outboundProxyURL)
	}
	if proxyURL == "" {
		return nil, nil
	}
	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("parse Responses WebSocket proxy: %w", err)
	}
	now := time.Now()
	d.proxyMu.Lock()
	defer d.proxyMu.Unlock()
	if d.closed {
		return nil, errors.New("Responses WebSocket dialer is closed")
	}
	if d.proxyClients == nil {
		d.proxyClients = make(map[string]*responsesWebSocketProxyClientEntry)
	}
	if entry := d.proxyClients[proxyURL]; entry != nil && entry.client != nil {
		entry.lastUsed = now
		return entry.client, nil
	}
	d.pruneProxyClientsLocked(now)
	transport := &http.Transport{
		Proxy:               http.ProxyURL(parsed),
		MaxIdleConns:        wsProxyMaxIdleConns,
		MaxIdleConnsPerHost: wsProxyMaxIdlePerHost,
		IdleConnTimeout:     wsProxyIdleConnTimeout,
		TLSHandshakeTimeout: defaultWSDialTimeout,
		ForceAttemptHTTP2:   true,
	}
	client := &http.Client{Transport: transport}
	d.proxyClients[proxyURL] = &responsesWebSocketProxyClientEntry{client: client, lastUsed: now}
	d.enforceProxyClientCapacityLocked()
	return client, nil
}

func (d *coderResponsesWebSocketDialer) pruneProxyClientsLocked(now time.Time) {
	for proxyURL, entry := range d.proxyClients {
		if entry == nil || entry.client == nil || now.Sub(entry.lastUsed) > wsProxyClientCacheTTL {
			closeResponsesWebSocketProxyClient(entry)
			delete(d.proxyClients, proxyURL)
		}
	}
}

func (d *coderResponsesWebSocketDialer) enforceProxyClientCapacityLocked() {
	for len(d.proxyClients) > wsProxyClientCacheLimit {
		var oldestURL string
		var oldestTime time.Time
		for proxyURL, entry := range d.proxyClients {
			lastUsed := time.Time{}
			if entry != nil {
				lastUsed = entry.lastUsed
			}
			if oldestURL == "" || lastUsed.Before(oldestTime) {
				oldestURL, oldestTime = proxyURL, lastUsed
			}
		}
		if oldestURL == "" {
			return
		}
		closeResponsesWebSocketProxyClient(d.proxyClients[oldestURL])
		delete(d.proxyClients, oldestURL)
	}
}

func (d *coderResponsesWebSocketDialer) Close() {
	if d == nil {
		return
	}
	d.proxyMu.Lock()
	defer d.proxyMu.Unlock()
	if d.closed {
		return
	}
	d.closed = true
	for proxyURL, entry := range d.proxyClients {
		closeResponsesWebSocketProxyClient(entry)
		delete(d.proxyClients, proxyURL)
	}
}

func closeResponsesWebSocketProxyClient(entry *responsesWebSocketProxyClientEntry) {
	if entry == nil || entry.client == nil || entry.client.Transport == nil {
		return
	}
	if transport, ok := entry.client.Transport.(interface{ CloseIdleConnections() }); ok {
		transport.CloseIdleConnections()
	}
}

func responseHeader(response *http.Response) http.Header {
	if response == nil {
		return nil
	}
	return response.Header.Clone()
}
