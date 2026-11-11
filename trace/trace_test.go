package trace

import (
	"strings"
	"sync"
	"testing"
)

func TestACallKeepsTheGoroutineThatBeganItAndWhereThatOneWasStarted(t *testing.T) {
	Enable("shop")
	began := make(chan int64, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { began <- Begin() })
	}
	wg.Wait()
	close(began)
	for n := range began {
		Write(Record{Seq: n, Dir: "out", Kind: "http", Name: "GET /images"})
	}
	Write(Record{Dir: "out", Kind: "http", Name: "GET /stock"})

	records := Records()
	images, stock := records[len(records)-3:len(records)-1], records[len(records)-1]
	if !strings.HasPrefix(images[0].Spawned, "sync.(*WaitGroup).Go in goroutine ") || images[0].Spawned != images[1].Spawned {
		t.Errorf("spawned %q and %q, want both by the wait group", images[0].Spawned, images[1].Spawned)
	}
	if images[0].Goroutine == 0 || images[0].Goroutine == images[1].Goroutine {
		t.Errorf("goroutines %d and %d, want two", images[0].Goroutine, images[1].Goroutine)
	}
	if stock.Goroutine == images[0].Goroutine || stock.Spawned == images[0].Spawned {
		t.Errorf("the test's own call is kept as %d %q", stock.Goroutine, stock.Spawned)
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
