package surface

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestVerifyClosesAPairedSet(t *testing.T) {
	r, err := Verify([]Surface{
		{Service: "orders", Publishes: []string{"orders.placed"}, Calls: []string{"/stock.S/Reserve"}},
		{Service: "stock", Consumes: []string{"orders.placed"}, Serves: []string{"/stock.S/Reserve"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Failed() || r.String() != "" {
		t.Errorf("want a closed set, got:\n%s", r)
	}
}

func TestVerifyFindsWhatHasNoPartner(t *testing.T) {
	r, err := Verify([]Surface{
		{Service: "orders", Publishes: []string{"orders.placed"}, Consumes: []string{"stock.reserved"}, Calls: []string{"/stock.S/Reserve"}},
		{Service: "stock", Serves: []string{"/stock.S/Release"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := Report{
		Unconsumed:  []string{"orders.placed (from orders)"},
		Unpublished: []string{"stock.reserved (by orders)"},
		Unserved:    []string{"/stock.S/Reserve (by orders)"},
		Uncalled:    []string{"/stock.S/Release (on stock)"},
	}
	if !reflect.DeepEqual(r, want) {
		t.Errorf("got  %+v\nwant %+v", r, want)
	}
}

func TestVerifyTellsLoopsAndWildcardsWithoutFailing(t *testing.T) {
	r, err := Verify([]Surface{
		{Service: "dungeon", Publishes: []string{"invoices.paid", "contracts.moved"}, Consumes: []string{"invoices.paid"}},
		{Service: "herald", Consumes: []string{"#"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := Report{
		PatternOnly: []string{"contracts.moved (from dungeon, taken by herald)"},
		Loops:       []string{"dungeon: invoices.paid"},
	}
	if r.Failed() || !reflect.DeepEqual(r, want) {
		t.Errorf("got  %+v\nwant %+v", r, want)
	}
}

func TestVerifyMatchesRequestsToRoutesAndLetsTheRestLeave(t *testing.T) {
	r, err := Verify([]Surface{
		{Service: "herald", Calls: []string{
			"POST /api/render/text", "GET /documents/{}", "GET /elsewhere", "GET payments/{}",
			"POST https://api.acquirer.test/v3/payments", "GET external", "GET /s/links/{}",
		}},
		{Service: "renderer", Serves: []string{"POST /api/render/text", "GET /documents/{id}"}},
		{Service: "archive", Serves: []string{"GET /documents/{id}"}},
		{Service: "media", Serves: []string{"GET /*"}},
		{Service: "shortener", Serves: []string{"GET {}/links/{code}"}},
		{Service: "apostle", Serves: []string{"GET /admin/users/{userId}"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := Report{
		Ambiguous: []string{"GET /documents/{} (by herald, routed by archive, renderer)"},
		Outside: []string{
			"GET /elsewhere (by herald)",
			"GET /s/links/{} (by herald)",
			"GET external (by herald)",
			"GET payments/{} (by herald)",
			"POST https://api.acquirer.test/v3/payments (by herald)",
		},
	}
	if !reflect.DeepEqual(r, want) {
		t.Errorf("got  %+v\nwant %+v", r, want)
	}
}

func TestVerifyLandsAClientsRequestsOnItsHost(t *testing.T) {
	r, err := Verify([]Surface{
		{Service: "portal", Clients: map[string][]string{"lib/http-client.httpClient": {
			"GET /customers/contracts/{}", "GET /customers/invoices/{}", "GET /rest/prices", "POST /customers/feedback",
		}}},
		{Service: "apostle", Serves: []string{"GET /customers/contracts/{contractId}", "GET /customers/invoices/{invoiceId}", "GET /rest/prices"}},
		{Service: "media", Serves: []string{"GET /customers/{customerId}/*"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := Report{Unserved: []string{"POST /customers/feedback (by portal, not on apostle)"}}
	if !reflect.DeepEqual(r, want) {
		t.Errorf("got  %+v\nwant %+v", r, want)
	}
}

func TestVerifyFailsOnAClientWithNoHost(t *testing.T) {
	r, err := Verify([]Surface{
		{Service: "herald", Clients: map[string][]string{
			"adapter.renderer.client": {"GET /documents/{}"},
			"adapter.download.client": {"GET external"},
			"driver.sendSms.client":   {"GET sms/send"},
		}},
		{Service: "renderer", Serves: []string{"GET /documents/{id}"}},
		{Service: "archive", Serves: []string{"GET /documents/{id}"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := Report{
		Ambiguous: []string{"GET /documents/{} (by herald, routed by archive, renderer)"},
		Hostless:  []string{"herald: adapter.download.client", "herald: driver.sendSms.client"},
	}
	if !reflect.DeepEqual(r, want) {
		t.Errorf("got  %+v\nwant %+v", r, want)
	}
}

func TestVerifyTakesAClientsHostFromFlowsClients(t *testing.T) {
	r, err := Verify([]Surface{
		{
			Service: "herald",
			Clients: map[string][]string{
				"adapter.archive.client":  {"GET /documents/{}", "GET /files/{}"},
				"adapter.download.client": {"GET external"},
				"driver.sendSms.client":   {"GET sms/send"},
			},
			Hosts: map[string]string{
				"adapter.archive.client":  "archive",
				"adapter.download.client": "suppliers",
				"driver.sendSms.client":   "sms.ru",
			},
		},
		{Service: "renderer", Serves: []string{"GET /documents/{id}"}},
		{Service: "archive", Serves: []string{"GET /documents/{id}"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := Report{
		Unserved: []string{"GET /files/{} (by herald, not on archive)"},
		Outside:  []string{"GET external (by herald, to suppliers)", "GET sms/send (by herald, to sms.ru)"},
	}
	if !reflect.DeepEqual(r, want) {
		t.Errorf("got  %+v\nwant %+v", r, want)
	}
}

func TestVerifyFailsOnAHostNamedWithoutNeed(t *testing.T) {
	r, err := Verify([]Surface{
		{
			Service: "portal",
			Clients: map[string][]string{"lib/http-client.httpClient": {"GET /rest/prices"}},
			Hosts:   map[string]string{"lib/http-client.httpClient": "apostle", "lib/gone.client": "apostle"},
		},
		{Service: "apostle", Serves: []string{"GET /rest/prices"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := Report{Needless: []string{
		"portal: lib/gone.client (no such client)",
		"portal: lib/http-client.httpClient (the set tells apostle)",
	}}
	if !reflect.DeepEqual(r, want) {
		t.Errorf("got  %+v\nwant %+v", r, want)
	}
}

func TestConfigRefusesAClientWithoutAHost(t *testing.T) {
	for _, clients := range []map[string]string{{"": "archive"}, {"adapter.archive.client": " "}} {
		if err := (Config{Clients: clients}).Validate(); err == nil {
			t.Errorf("%v: want a refusal", clients)
		}
	}
}

func TestLoadConfigReadsTheFlowsGroupAlone(t *testing.T) {
	dir := t.TempDir()
	body := "agents:\n  areas: {}\nflows:\n  clients:\n    adapter.archive.client: archive\n"
	if err := os.WriteFile(filepath.Join(dir, ConfigFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"adapter.archive.client": "archive"}; !reflect.DeepEqual(cfg.Clients, want) {
		t.Errorf("got %v, want %v", cfg.Clients, want)
	}
	if cfg, err := LoadConfig(t.TempDir()); err != nil || cfg.Clients != nil {
		t.Errorf("without a file: %v, %v; want nothing", cfg, err)
	}
}

func TestRouteMatching(t *testing.T) {
	for _, c := range []struct {
		route, request string
		want           bool
	}{
		{"GET /users/{id}", "GET /users/{}", true},
		{"GET /users/{id}", "GET /users/me", true},
		{"GET /users/me", "GET /users/{}", true},
		{"GET /users/{id}", "POST /users/{}", false},
		{"GET /*", "GET /a/b/c", true},
		{"GET /files/*", "GET /files", false},
		{"GET /users", "GET /users/{}", false},
		{"POST /api/render/text", "POST /api/render/template", false},
	} {
		if got := routeMatches(c.route, c.request); got != c.want {
			t.Errorf("routeMatches(%q, %q) = %v, want %v", c.route, c.request, got, c.want)
		}
	}
}

func TestReceiversAndSenders(t *testing.T) {
	set := []Surface{
		{Service: "dungeon", Publishes: []string{"contracts.moved"}, Calls: []string{"/shark.Pay/Charge", "POST /api/render/text"}},
		{Service: "bastion", Consumes: []string{"contracts.moved"}},
		{Service: "herald", Consumes: []string{"#"}},
		{Service: "shark", Serves: []string{"/shark.Pay/Charge", "GET /*"}},
		{Service: "renderer", Serves: []string{"POST /api/render/{kind}"}},
	}
	for _, c := range []struct {
		kind, name  string
		exact, wild []string
	}{
		{"event", "contracts.moved", []string{"bastion"}, []string{"herald"}},
		{"grpc", "/shark.Pay/Charge", []string{"shark"}, nil},
		{"http", "POST /api/render/text", []string{"renderer"}, nil},
		{"http", "GET /anything", nil, nil},
	} {
		exact, wild := Receivers(set, "dungeon", c.kind, c.name)
		if !reflect.DeepEqual(exact, c.exact) || !reflect.DeepEqual(wild, c.wild) {
			t.Errorf("Receivers(%s %s) = %v, %v; want %v, %v", c.kind, c.name, exact, wild, c.exact, c.wild)
		}
	}
	if got := Senders(set, "renderer", "http", "POST /api/render/{kind}"); !reflect.DeepEqual(got, []string{"dungeon"}) {
		t.Errorf("Senders(http) = %v, want [dungeon]", got)
	}
	if got := Senders(set, "bastion", "event", "contracts.moved"); !reflect.DeepEqual(got, []string{"dungeon"}) {
		t.Errorf("Senders(event) = %v, want [dungeon]", got)
	}
}

func TestReceiversAndSendersKeepAClientToItsHost(t *testing.T) {
	set := []Surface{
		{Service: "portal", Clients: map[string][]string{"lib/http-client.httpClient": {"GET /customers/contracts/{}", "GET /rest/prices"}}},
		{Service: "apostle", Serves: []string{"GET /customers/contracts/{contractId}", "GET /rest/prices"}},
		{Service: "media", Serves: []string{"GET /customers/{customerId}/*"}},
	}
	if exact, _ := Receivers(set, "portal", "http", "GET /customers/contracts/{contractId}"); !reflect.DeepEqual(exact, []string{"apostle"}) {
		t.Errorf("Receivers = %v, want [apostle]", exact)
	}
	if got := Senders(set, "apostle", "http", "GET /customers/contracts/{contractId}"); !reflect.DeepEqual(got, []string{"portal"}) {
		t.Errorf("Senders(apostle) = %v, want [portal]", got)
	}
	if got := Senders(set, "media", "http", "GET /customers/{customerId}/*"); got != nil {
		t.Errorf("Senders(media) = %v, want none", got)
	}
}

func TestVerifyRefusesAServiceTwice(t *testing.T) {
	if _, err := Verify([]Surface{{Service: "a"}, {Service: "a"}}); err == nil {
		t.Fatal("want a refusal of the duplicate")
	}
}

func TestTopicMatching(t *testing.T) {
	for _, c := range []struct {
		pattern, key string
		want         bool
	}{
		{"#", "a.b.c", true},
		{"a.#", "a", true},
		{"a.#", "a.b.c", true},
		{"a.*", "a.b", true},
		{"a.*", "a.b.c", false},
		{"a.*", "a", false},
		{"*.b.#", "a.b", true},
		{"a.b", "a.c", false},
	} {
		if got := matches(c.pattern, c.key); got != c.want {
			t.Errorf("matches(%q, %q) = %v, want %v", c.pattern, c.key, got, c.want)
		}
	}
}
