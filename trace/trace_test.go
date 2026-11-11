package trace

import "testing"

func TestRawKeepsJSONAndQuotesText(t *testing.T) {
	for body, want := range map[string]string{
		`{"id":7}`: `{"id":7}`,
		"plain":    `"plain"`,
		"":         "",
	} {
		if got := string(Raw([]byte(body))); got != want {
			t.Errorf("Raw(%q) = %s, want %s", body, got, want)
		}
	}
}
