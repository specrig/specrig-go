package surface

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
)

type Report struct {
	Unconsumed  []string `yaml:"unconsumed,omitempty"`
	Unpublished []string `yaml:"unpublished,omitempty"`
	Unserved    []string `yaml:"unserved,omitempty"`
	Uncalled    []string `yaml:"uncalled,omitempty"`
	Ambiguous   []string `yaml:"ambiguous,omitempty"`
	Hostless    []string `yaml:"hostless,omitempty"`
	Needless    []string `yaml:"needless,omitempty"`

	PatternOnly []string `yaml:"pattern_only,omitempty"`
	Loops       []string `yaml:"loops,omitempty"`
	Outside     []string `yaml:"outside,omitempty"`
}

func (r Report) Failed() bool {
	return len(r.Unconsumed)+len(r.Unpublished)+len(r.Unserved)+len(r.Uncalled)+len(r.Ambiguous)+len(r.Hostless)+len(r.Needless) > 0
}

func (r Report) String() string {
	var b strings.Builder
	section := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(&b, "%s:\n", title)
		for _, i := range items {
			fmt.Fprintf(&b, "  %s\n", i)
		}
	}
	section("published, no subscriber", r.Unconsumed)
	section("subscribed, nobody publishes", r.Unpublished)
	section("called, nobody serves", r.Unserved)
	section("served, nobody calls", r.Uncalled)
	section("requested, routed by more than one service", r.Ambiguous)
	section("a client no service routes, not named in flows.clients", r.Hostless)
	section("named in flows.clients, not needed", r.Needless)
	section("taken only by a wildcard subscription", r.PatternOnly)
	section("consumed by the service that publishes it", r.Loops)
	section("requested outside the set", r.Outside)
	return b.String()
}

func Verify(set []Surface) (Report, error) {
	seen := map[string]bool{}
	for _, s := range set {
		if seen[s.Service] {
			return Report{}, fmt.Errorf("service %s appears twice in the set", s.Service)
		}
		seen[s.Service] = true
	}

	type party = map[string][]string
	publishers, consumers, servers, callers := party{}, party{}, party{}, party{}
	for _, s := range set {
		for _, k := range s.Publishes {
			publishers[k] = append(publishers[k], s.Service)
		}
		for _, k := range s.Consumes {
			consumers[k] = append(consumers[k], s.Service)
		}
		for _, m := range s.Serves {
			servers[m] = append(servers[m], s.Service)
		}
		for _, m := range s.Calls {
			callers[m] = append(callers[m], s.Service)
		}
	}

	var r Report
	for _, key := range sorted(publishers) {
		var exact, wild []string
		for _, sub := range sorted(consumers) {
			switch {
			case sub == key:
				exact = append(exact, consumers[sub]...)
			case matches(sub, key):
				wild = append(wild, consumers[sub]...)
			}
		}
		from := strings.Join(publishers[key], ", ")
		switch {
		case len(exact) == 0 && len(wild) == 0:
			r.Unconsumed = append(r.Unconsumed, fmt.Sprintf("%s (from %s)", key, from))
		case len(exact) == 0:
			r.PatternOnly = append(r.PatternOnly, fmt.Sprintf("%s (from %s, taken by %s)", key, from, strings.Join(wild, ", ")))
		}
	}
	for _, sub := range sorted(consumers) {
		if wildcard(sub) || publishers[sub] != nil {
			continue
		}
		r.Unpublished = append(r.Unpublished, fmt.Sprintf("%s (by %s)", sub, strings.Join(consumers[sub], ", ")))
	}
	for _, m := range sorted(callers) {
		if !isHTTP(m) && servers[m] == nil {
			r.Unserved = append(r.Unserved, fmt.Sprintf("%s (by %s)", m, strings.Join(callers[m], ", ")))
		}
	}
	type finding struct{ section, request, tail string }
	found := map[finding][]string{}
	for _, s := range set {
		for _, q := range outgoing(set, s) {
			var f finding
			switch {
			case q.hostless, len(q.to) == 1:
				continue
			case len(q.to) > 1:
				f = finding{"ambiguous", q.name, ", routed by " + strings.Join(q.to, ", ")}
			case q.outside:
				f = finding{"outside", q.name, ", to " + q.host}
			case q.host != "" && known(q.name):
				f = finding{"unserved", q.name, ", not on " + q.host}
			default:
				f = finding{"outside", q.name, ""}
			}
			found[f] = append(found[f], s.Service)
		}
		for _, client := range sorted(s.Clients) {
			if _, named := s.Hosts[client]; !named && len(hosts(set, s.Clients[client])) == 0 {
				r.Hostless = append(r.Hostless, fmt.Sprintf("%s: %s", s.Service, client))
			}
		}
		for _, client := range slices.Sorted(maps.Keys(s.Hosts)) {
			switch {
			case s.Clients[client] == nil:
				r.Needless = append(r.Needless, fmt.Sprintf("%s: %s (no such client)", s.Service, client))
			case slices.Equal(hosts(set, s.Clients[client]), []string{s.Hosts[client]}):
				r.Needless = append(r.Needless, fmt.Sprintf("%s: %s (the set tells %s)", s.Service, client, s.Hosts[client]))
			}
		}
	}
	findings := slices.SortedFunc(maps.Keys(found), func(a, b finding) int {
		return cmp.Or(strings.Compare(a.request, b.request), strings.Compare(a.tail, b.tail))
	})
	for _, f := range findings {
		line := fmt.Sprintf("%s (by %s%s)", f.request, strings.Join(slices.Compact(found[f]), ", "), f.tail)
		switch f.section {
		case "ambiguous":
			r.Ambiguous = append(r.Ambiguous, line)
		case "unserved":
			r.Unserved = append(r.Unserved, line)
		default:
			r.Outside = append(r.Outside, line)
		}
	}
	// A route nobody in the set requests is a way in from outside, so only RPCs
	// are held to having a caller.
	for _, m := range sorted(servers) {
		if !isHTTP(m) && callers[m] == nil {
			r.Uncalled = append(r.Uncalled, fmt.Sprintf("%s (on %s)", m, strings.Join(servers[m], ", ")))
		}
	}
	for _, s := range set {
		for _, k := range s.Publishes {
			if slices.Contains(s.Consumes, k) {
				r.Loops = append(r.Loops, fmt.Sprintf("%s: %s", s.Service, k))
			}
		}
	}
	return r, nil
}

type request struct {
	name     string
	host     string
	outside  bool
	hostless bool // the client is what is reported, not each of its requests
	to       []string
}

func outgoing(set []Surface, s Surface) []request {
	var out []request
	for _, m := range s.Calls {
		if !isHTTP(m) {
			continue
		}
		q := request{name: m, to: routing(set, m)}
		if len(q.to) == 1 {
			q.host = q.to[0]
		}
		out = append(out, q)
	}
	for _, client := range sorted(s.Clients) {
		told := hosts(set, s.Clients[client])
		host := s.Hosts[client]
		if host == "" && len(told) == 1 {
			host = told[0]
		}
		for _, m := range s.Clients[client] {
			q := request{name: m, host: host, to: routing(set, m)}
			switch {
			case host != "" && !slices.ContainsFunc(set, func(o Surface) bool { return o.Service == host }):
				q.outside, q.to = true, nil
			case host != "":
				q.to = slices.DeleteFunc(q.to, func(svc string) bool { return svc != host })
			case len(told) == 0:
				q.hostless = true
			}
			out = append(out, q)
		}
	}
	return out
}

func hosts(set []Surface, requests []string) []string {
	count := map[string]int{}
	for _, m := range requests {
		for _, svc := range routing(set, m) {
			count[svc]++
		}
	}
	var best []string
	for _, svc := range slices.Sorted(maps.Keys(count)) {
		switch {
		case len(best) == 0 || count[svc] > count[best[0]]:
			best = []string{svc}
		case count[svc] == count[best[0]]:
			best = append(best, svc)
		}
	}
	return best
}

func routing(set []Surface, request string) []string {
	if !known(request) {
		return nil
	}
	var out []string
	for _, s := range set {
		if slices.ContainsFunc(s.Serves, func(route string) bool {
			return isHTTP(route) && names(route) && routeMatches(route, request)
		}) {
			out = append(out, s.Service)
		}
	}
	slices.Sort(out)
	return out
}

func Receivers(set []Surface, from, kind, name string) (exact, wild []string) {
	for _, s := range set {
		switch kind {
		case "event":
			for _, sub := range s.Consumes {
				switch {
				case sub == name:
					exact = append(exact, s.Service)
				case wildcard(sub) && matches(sub, name):
					wild = append(wild, s.Service)
				}
			}
		case "grpc":
			if slices.Contains(s.Serves, name) {
				exact = append(exact, s.Service)
			}
		case "http":
			for _, route := range s.Serves {
				if isHTTP(route) && known(name) && names(route) && routeMatches(route, name) && landsOn(set, from, name, s.Service) {
					exact = append(exact, s.Service)
				}
			}
		}
	}
	slices.Sort(exact)
	slices.Sort(wild)
	return slices.Compact(exact), slices.Compact(slices.DeleteFunc(wild, func(w string) bool { return slices.Contains(exact, w) }))
}

func landsOn(set []Surface, from, name, to string) bool {
	for _, s := range set {
		if s.Service != from {
			continue
		}
		for _, q := range outgoing(set, s) {
			if q.host != "" && q.host != to && routeMatches(name, q.name) {
				return false
			}
		}
	}
	return true
}

// For HTTP, name is a route of service to, where Receivers takes a request.
func Senders(set []Surface, to, kind, name string) []string {
	var out []string
	for _, s := range set {
		switch kind {
		case "event":
			if slices.Contains(s.Publishes, name) {
				out = append(out, s.Service)
			}
		case "grpc":
			if slices.Contains(s.Calls, name) {
				out = append(out, s.Service)
			}
		case "http":
			for _, q := range outgoing(set, s) {
				if known(q.name) && routeMatches(name, q.name) && (q.host == "" || q.host == to) {
					out = append(out, s.Service)
				}
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func wildcard(sub string) bool {
	for _, w := range strings.Split(sub, ".") {
		if w == "*" || w == "#" {
			return true
		}
	}
	return false
}

// The AMQP topic rule.
func matches(pattern, key string) bool {
	return topic(strings.Split(pattern, "."), strings.Split(key, "."))
}

func topic(p, k []string) bool {
	switch {
	case len(p) == 0:
		return len(k) == 0
	case p[0] == "#":
		for i := 0; i <= len(k); i++ {
			if topic(p[1:], k[i:]) {
				return true
			}
		}
		return false
	case len(k) == 0:
		return false
	case p[0] == "*" || p[0] == k[0]:
		return topic(p[1:], k[1:])
	}
	return false
}

func sorted(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
