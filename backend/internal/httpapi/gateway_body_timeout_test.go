package httpapi

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestGatewayBodyReadTimeoutClassification(t *testing.T) {
	err := fmt.Errorf("reading upload: %w", &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded})
	status, code, message := gatewayBodyReadError(err)
	if status != 408 || code != "request_body_timeout" || !strings.Contains(message, "not been forwarded upstream") {
		t.Fatalf("%d %s %s", status, code, message)
	}
}

func TestGatewayBodyReadDeadlineOnRealConnection(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(fmt.Sprint(timeout), func(t *testing.T) {
			api := &Server{gatewayBodyReadTimeout: time.Second}
			if timeout {
				api.SetGatewayBodyReadTimeout(100 * time.Millisecond)
			}
			started := make(chan struct{})
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(started)
				body, err := api.readGatewayBody(w, r, maxGatewayBody)
				if err != nil {
					status, code, message := gatewayBodyReadError(err)
					writeGatewayErrorStatus(w, status, code, message)
					return
				}
				w.Write(body)
			}))
			server.Config.ReadTimeout = 50 * time.Millisecond
			server.Start()
			defer server.Close()
			conn, err := net.Dial("tcp", server.Listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(5 * time.Second))
			fmt.Fprint(conn, "POST / HTTP/1.1\r\nHost: test\r\nContent-Length: 2\r\n\r\na")
			<-started
			if !timeout {
				time.Sleep(150 * time.Millisecond)
				fmt.Fprint(conn, "b")
			}
			response, err := http.ReadResponse(bufio.NewReader(conn), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if timeout {
				if !response.Close {
					t.Fatal("timed-out upload must close the connection")
				}
				if response.StatusCode != 408 || !strings.Contains(string(body), "request_body_timeout") {
					t.Fatalf("%d %s", response.StatusCode, body)
				}
			} else if response.StatusCode != 200 || string(body) != "ab" {
				t.Fatalf("%d %s", response.StatusCode, body)
			}
		})
	}
}

func TestGatewayBodyReadConnectionResetIsNotTimeout(t *testing.T) {
	status, code, _ := gatewayBodyReadError(&net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET})
	if status != http.StatusBadRequest || code != "invalid_request_error" {
		t.Fatalf("%d %s", status, code)
	}
}
