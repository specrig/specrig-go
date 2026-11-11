package standin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
)

type document struct {
	spec      *openapi3.T
	templates []template
	bases     []string
	options   *openapi3filter.Options
}

type template struct {
	path string
	item *openapi3.PathItem
}

type found struct {
	*routers.Route
	base   string
	params map[string]string
}

var (
	documents   = map[string]*document{}
	documentsMu sync.Mutex
)

func load(file string) (*document, error) {
	abs, err := filepath.Abs(file)
	if err != nil {
		return nil, err
	}
	documentsMu.Lock()
	defer documentsMu.Unlock()
	if d, ok := documents[abs]; ok {
		return d, nil
	}
	spec, err := openapi3.NewLoader().LoadFromFile(abs)
	if err != nil {
		return nil, err
	}
	// An example that does not fit its schema is the neighbour's business, not a reason to refuse its contract.
	if err := spec.Validate(context.Background(), openapi3.DisableExamplesValidation()); err != nil {
		return nil, err
	}
	d := &document{
		spec: spec,
		options: &openapi3filter.Options{
			AuthenticationFunc:    openapi3filter.NoopAuthenticationFunc,
			IncludeResponseStatus: true,
			MultiError:            true,
			SkipSettingDefaults:   true,
		},
	}
	// The details dump the schema and the value into every line; 3.1's validator refuses the option.
	if !spec.IsOpenAPI31OrLater() {
		d.options.SchemaValidationOptions = []openapi3.SchemaValidationOption{openapi3.DisableSchemaErrorDetails()}
	}
	for _, path := range spec.Paths.InMatchingOrder() {
		d.templates = append(d.templates, template{path, spec.Paths.Value(path)})
	}
	for _, server := range spec.Servers {
		base, err := server.BasePath()
		if err != nil {
			return nil, fmt.Errorf("server %s: %w", server.URL, err)
		}
		d.bases = append(d.bases, strings.TrimRight(base, "/"))
	}
	if len(d.bases) == 0 {
		d.bases = []string{""}
	}
	slices.SortStableFunc(d.bases, func(a, b string) int { return len(b) - len(a) })
	documents[abs] = d
	return d, nil
}

func (d *document) schema(name string) *openapi3.SchemaRef {
	if d.spec.Components == nil {
		return nil
	}
	return d.spec.Components.Schemas[name]
}

// Matched by hand: the routers of kin-openapi take the host of a server into account, and a request's
// host is whatever the service was configured with.
func (d *document) find(method, path string) *found {
	if d == nil {
		return nil
	}
	for _, base := range d.bases {
		rest, ok := strings.CutPrefix(path, base)
		if !ok || (rest != "" && rest[0] != '/') {
			continue
		}
		if rest == "" {
			rest = "/"
		}
		for _, t := range d.templates {
			operation := t.item.GetOperation(method)
			if operation == nil {
				continue
			}
			if params, ok := bind(t.path, rest); ok {
				return &found{&routers.Route{Spec: d.spec, Path: t.path, PathItem: t.item, Method: method, Operation: operation}, base, params}
			}
		}
	}
	return nil
}

func (d *document) request(f *found, req *http.Request, body []byte) string {
	checked := req.Clone(req.Context())
	checked.Body = io.NopCloser(bytes.NewReader(body))
	checked.ContentLength = int64(len(body))
	checked.GetBody = nil
	err := openapi3filter.ValidateRequest(req.Context(), d.input(f, checked))
	return flatten(err)
}

func (d *document) response(f *found, req *http.Request, status int, header http.Header, body []byte, failure *openapi3.SchemaRef) string {
	responses := f.Operation.Responses
	if failure != nil && status >= 500 && responses.Len() > 0 && responses.Status(status) == nil && responses.Default() == nil {
		return d.failure(failure, body)
	}
	input := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: d.input(f, req),
		Status:                 status,
		Header:                 header,
		Options:                d.options,
	}
	input.SetBodyBytes(body)
	return flatten(openapi3filter.ValidateResponse(req.Context(), input))
}

func (d *document) failure(schema *openapi3.SchemaRef, body []byte) string {
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return "the failure is not JSON"
	}
	opts := append(slices.Clone(d.options.SchemaValidationOptions), openapi3.VisitAsResponse())
	if d.spec.IsOpenAPI31OrLater() {
		opts = append(opts, openapi3.EnableJSONSchema2020())
	}
	if err := schema.Value.VisitJSON(value, opts...); err != nil {
		return "the failure does not match its schema: " + flatten(err)
	}
	return ""
}

func (d *document) input(f *found, req *http.Request) *openapi3filter.RequestValidationInput {
	return &openapi3filter.RequestValidationInput{Request: req, PathParams: f.params, Route: f.Route, Options: d.options}
}

func flatten(err error) string {
	if err == nil {
		return ""
	}
	return strings.Join(strings.Fields(err.Error()), " ")
}
