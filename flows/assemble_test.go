package flows

import (
	"reflect"
	"strings"
	"testing"

	"github.com/specrig/specrig-go/surface"
)

func traced(services ...string) project {
	p := project{services: map[string]service{}}
	for _, s := range services {
		p.services[s] = service{}
	}
	return p
}

func TestBeyondTellsWhereAnUnfollowedMessageGoes(t *testing.T) {
	p := traced("dungeon", "warehouse")
	p.surfaces = []surface.Surface{
		{Service: "dungeon", Publishes: []string{"contracts.moved"}, Calls: []string{"/warehouse.Units/Get"}},
		{Service: "warehouse", Serves: []string{"/warehouse.Units/Get"}},
		{Service: "herald", Consumes: []string{"#"}},
	}
	for m, want := range map[string]string{
		"grpc /warehouse.Units/Get": "received by warehouse, no case took it",
		"event contracts.moved":     "only a wildcard subscription: herald",
		"grpc /nobody.Serves/This":  "out of the system",
	} {
		if got := p.beyond("dungeon", m); got != want {
			t.Errorf("beyond(%s) = %q, want %q", m, got, want)
		}
	}
	if got := traced().beyond("dungeon", "event contracts.moved"); got != "not traced" {
		t.Errorf("without records: %q, want not traced", got)
	}
}

// A service without a trace cannot have followed anything, so only links between
// two traced services are held against the cases.
func TestUntestedIsWhatTheCodeLinksAndNoCaseFollows(t *testing.T) {
	p := traced("dungeon", "herald", "warehouse")
	p.surfaces = []surface.Surface{
		{Service: "apostle", Calls: []string{"/contract.Contracts/Get"}},
		{Service: "dungeon", Publishes: []string{"invoices.paid"}, Serves: []string{"/contract.Contracts/Get"}, Calls: []string{"/warehouse.Units/Get"}},
		{Service: "herald", Consumes: []string{"invoices.paid"}, Calls: []string{"/contract.Contracts/Get", "POST /api/render/text"}},
		{Service: "warehouse", Serves: []string{"/warehouse.Units/Get"}},
	}
	followed := []Link{{From: "dungeon", To: "herald", Message: "event invoices.paid"}}
	want := []string{
		"dungeon → warehouse: grpc /warehouse.Units/Get",
		"herald → dungeon: grpc /contract.Contracts/Get",
	}
	if got := p.untested(followed); !reflect.DeepEqual(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
}

func TestPagesAreNamedApartAndInASCII(t *testing.T) {
	steps := map[string]step{}
	for _, m := range []string{
		"http POST /orders",
		`ui the visitor clicks "Оплатить"`,
		`ui the visitor clicks "СБП"`,
		`ui the visitor applies promo code " summer "`,
		`ui the visitor applies promo code "SUMMER"`,
		"grpc /document.TemplateService/Create",
		"grpc /notification.TemplateService/Create",
	} {
		s := step{service: "portal", input: Input{Message: m}}
		steps[s.id()] = s
	}
	got := names(steps)
	for id, want := range map[string]string{
		"portal: grpc /document.TemplateService/Create":     "portal-grpc-document-templateservice-create",
		"portal: grpc /notification.TemplateService/Create": "portal-grpc-notification-templateservice-create",
		"portal: http POST /orders":                         "portal-http-post-orders",
		`portal: ui the visitor clicks "Оплатить"`:          "portal-ui-the-visitor-clicks-oplatit",
		`portal: ui the visitor clicks "СБП"`:               "portal-ui-the-visitor-clicks-sbp",
	} {
		if got[id] != want {
			t.Errorf("%s: %q, want %q", id, got[id], want)
		}
	}
	spaced, upper := got[`portal: ui the visitor applies promo code " summer "`], got[`portal: ui the visitor applies promo code "SUMMER"`]
	if spaced == upper || !strings.HasPrefix(spaced, "portal-ui-the-visitor-applies-promo-code-summer-") {
		t.Errorf("a name both share must part them: %q, %q", spaced, upper)
	}
	if again := names(steps); !reflect.DeepEqual(again, got) {
		t.Errorf("names moved between runs: %v, then %v", got, again)
	}
}

func TestSendersLeaveOutTheServiceItself(t *testing.T) {
	p := traced("dungeon")
	p.surfaces = []surface.Surface{
		{Service: "apostle", Calls: []string{"/checkout.Checkout/Pay"}},
		{Service: "dungeon", Serves: []string{"/checkout.Checkout/Pay"}, Calls: []string{"/checkout.Checkout/Pay"}},
	}
	s := step{service: "dungeon", input: Input{Message: "grpc /checkout.Checkout/Pay"}}
	if got := p.senders(s); !reflect.DeepEqual(got, []string{"apostle"}) {
		t.Errorf("got %v, want [apostle]", got)
	}
}

func TestAPageAssembleWroteIsOneToRemoveAndAProjectsOwnIsNot(t *testing.T) {
	if !Generated([]byte(pageHeader + "# a flow\n")) {
		t.Error("a page Assemble wrote is not recognised")
	}
	if Generated([]byte("# How the services talk\n")) {
		t.Error("a page of the project's own is taken for a generated one")
	}
}
