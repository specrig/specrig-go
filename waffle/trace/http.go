package trace

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	record "github.com/specrig/specrig-go/trace"
	"gitlab.com/gorib/waffle/tools/tap"
)

// HTTP records every request the suite sends to the service's own server, under
// the route it matched rather than the path, which carries ids.
func HTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !record.On() {
			next.ServeHTTP(w, r)
			return
		}
		began := record.Begin()
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		rec := &response{ResponseWriter: w, status: http.StatusOK}
		route := new(string)
		r = r.WithContext(tap.WithRoute(r.Context(), route))
		next.ServeHTTP(rec, r)

		name := r.Pattern
		if *route != "" {
			name = r.Method + " " + *route
		}
		if name == "" {
			name = r.Method + " " + r.URL.Path
		}
		if len(body) == 0 && r.URL.RawQuery != "" {
			body, _ = json.Marshal(r.URL.Query())
		}
		out := record.Record{Seq: began, Dir: "in", Kind: "http", Name: name, Request: record.Raw(body), Response: record.Raw(rec.body.Bytes())}
		if rec.status >= http.StatusBadRequest {
			out.Error = fmt.Sprintf("HTTP %d", rec.status)
			var answer struct{ Type string }
			if json.Unmarshal(rec.body.Bytes(), &answer) == nil && answer.Type != "" {
				out.Error += " " + answer.Type
			}
		}
		record.Write(out)
	})
}

type response struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (r *response) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *response) Write(b []byte) (int, error) {
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}
