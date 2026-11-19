package trace

import (
	"slices"
	"sync"
	"testing"
)

func TestACallKeepsEveryGoroutineThatLedToIt(t *testing.T) {
	Enable("shop")
	var carriers sync.WaitGroup
	carriers.Go(func() {
		var trucks sync.WaitGroup
		trucks.Go(func() { Write(Record{Dir: "out", Kind: "http", Name: "POST /rates"}) })
		trucks.Wait()
	})
	carriers.Wait()
	Write(Record{Dir: "out", Kind: "http", Name: "GET /stock"})

	records := Records()
	rates, stock := records[len(records)-2], records[len(records)-1]
	if len(rates.Threads) != len(stock.Threads)+2 || !slices.Equal(rates.Threads[2:], stock.Threads) {
		t.Errorf("the truck's call is kept on %v, the test's own on %v; want the truck, its carrier, then the test's", rates.Threads, stock.Threads)
	}
}

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
