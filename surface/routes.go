package surface

import "strings"

// What a writer puts for a part of a path configuration sets.
const configPrefix = "{}"

func routeMatches(route, request string) bool {
	rv, rp, _ := strings.Cut(route, " ")
	qv, qp, _ := strings.Cut(request, " ")
	if rv != qv && rv != "*" {
		return false
	}
	qp, _, _ = strings.Cut(qp, "?")
	rs, qs := segments(rp), segments(qp)
	for i, r := range rs {
		if i >= len(qs) {
			return false
		}
		if r == "*" && i == len(rs)-1 {
			return true
		}
		if strings.HasPrefix(r, "{") || qs[i] == "{}" {
			continue
		}
		if r != qs[i] {
			return false
		}
	}
	return len(rs) == len(qs)
}

// A path without a leading slash goes onto the client's base URL whole, path
// included, and that URL is configuration: where it lands cannot be told.
func known(request string) bool {
	_, path, _ := strings.Cut(request, " ")
	return strings.HasPrefix(path, "/")
}

// /* or /{x} takes anything and so names nothing, and under a prefix configuration
// sets nothing can be told: neither is evidence that a request lands on its service.
func names(route string) bool {
	_, path, _ := strings.Cut(route, " ")
	if strings.HasPrefix(path, configPrefix) {
		return false
	}
	for _, s := range segments(path) {
		if s != "" && s != "*" && !strings.HasPrefix(s, "{") {
			return true
		}
	}
	return false
}

func segments(path string) []string {
	return strings.Split(strings.Trim(path, "/"), "/")
}

func isHTTP(entry string) bool { return !strings.HasPrefix(entry, "/") }
