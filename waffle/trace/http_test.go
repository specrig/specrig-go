package trace

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAHandlerTakesItsConnectionOverUnderTheRecord(t *testing.T) {
	Enable("shop")
	server := httptest.NewServer(HTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		conn, buffered, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Errorf("the connection under the record cannot be taken over: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = buffered.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 5\r\nConnection: close\r\n\r\ntaken")
		_ = buffered.Flush()
	})))
	t.Cleanup(server.Close)

	response, err := http.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}

	if string(body) != "taken" {
		t.Errorf("the answer is %q, not the one the handler wrote on the connection", body)
	}
}
