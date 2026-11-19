package flows

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/specrig/specrig-go/trace"
)

// A suite may move the working directory to isolate its files; the package
// directory is where the test binary started.
var home, _ = os.Getwd()

// Format is what this package writes under specrig: and the newest it reads.
const Format = "flows/2"

type Doc struct {
	Specrig  string   `yaml:"specrig"`
	Service  string   `yaml:"service"`
	Inputs   []Input  `yaml:"inputs"`
	Outputs  []Output `yaml:"outputs,omitempty"`
	Orphaned []Orphan `yaml:"orphaned,omitempty"`
}

type Input struct {
	Message     string              `yaml:"message"`
	Description string              `yaml:"description,omitempty"`
	Fields      []string            `yaml:"fields,omitempty"`
	Needs       map[string][]string `yaml:"needs,omitempty"`
	Branches    []Branch            `yaml:"branches"`
}

type Branch struct {
	Sends       []Send              `yaml:"sends,omitempty"`
	Sent        map[string][]string `yaml:"sent,omitempty"`
	Refusal     string              `yaml:"refused,omitempty"`
	Description string              `yaml:"description,omitempty"`
	Cases       []string            `yaml:"cases"`
}

func (b Branch) refused() bool { return b.Refusal != "" }

func (b Branch) messages() []string { return messages(b.Sends) }

// A message sent, or the sequences that ran side by side.
type Send struct {
	Message  string
	Together [][]Send
}

func (s Send) MarshalYAML() (any, error) {
	if s.Together == nil {
		return s.Message, nil
	}
	return struct {
		Together [][]Send `yaml:"together"`
	}{s.Together}, nil
}

func (s *Send) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		return node.Decode(&s.Message)
	}
	var block struct {
		Together [][]Send `yaml:"together"`
	}
	if err := node.Decode(&block); err != nil {
		return err
	}
	s.Together = block.Together
	return nil
}

func sent(messages ...string) []Send {
	out := make([]Send, len(messages))
	for i, m := range messages {
		out[i] = Send{Message: m}
	}
	return out
}

func spell(seq []Send) string {
	parts := make([]string, len(seq))
	for i, s := range seq {
		if s.Together == nil {
			parts[i] = s.Message
			continue
		}
		branches := make([]string, len(s.Together))
		for j, b := range s.Together {
			branches[j] = spell(b)
		}
		parts[i] = "[" + strings.Join(branches, " & ") + "]"
	}
	return strings.Join(parts, ", ")
}

func messages(seq []Send) []string {
	var out []string
	for _, s := range seq {
		if s.Together == nil {
			out = append(out, s.Message)
		}
		for _, b := range s.Together {
			out = append(out, messages(b)...)
		}
	}
	return out
}

// seq without the messages drop matches, side by side as what is left ran.
func without(seq []Send, drop func(string) bool) []Send {
	var out []Send
	for _, s := range seq {
		if s.Together == nil {
			if !drop(s.Message) {
				out = append(out, s)
			}
			continue
		}
		left := make([][]Send, len(s.Together))
		for i, b := range s.Together {
			left[i] = without(b, drop)
		}
		out = append(out, together(left)...)
	}
	return out
}

// Sequences that ran side by side, each written once in the order of its spelling;
// one left alone is just itself.
func together(branches [][]Send) []Send {
	by := map[string][]Send{}
	for _, b := range branches {
		if len(b) > 0 {
			by[spell(b)] = b
		}
	}
	if len(by) < 2 {
		for _, b := range by {
			return b
		}
		return nil
	}
	out := make([][]Send, 0, len(by))
	for _, k := range sorted(keys(by)) {
		out = append(out, by[k])
	}
	return []Send{{Together: out}}
}

type Output struct {
	Message string   `yaml:"message"`
	Fields  []string `yaml:"fields,omitempty"`
}

type Orphan struct {
	Anchor      string `yaml:"anchor"`
	Title       string `yaml:"title,omitempty"`
	Description string `yaml:"description,omitempty"`
}

func message(kind, name string) string { return kind + " " + name }

func (i Input) anchor() string { return i.Message }

func (b Branch) anchor(in Input) string {
	a := in.Message + " -> " + spell(b.Sends)
	if b.refused() {
		a += " (refused " + b.Refusal + ")"
	}
	return a
}

// What each scenario received, and what it sent after it until the next thing
// it received.
func build(service string, records []trace.Record, cases features) Doc {
	type key struct {
		sends   string
		refusal string
	}
	inputs := map[string]*Input{}
	branches := map[string]map[key]*Branch{}
	outputs := map[string]map[string]bool{}
	fields := map[string]map[string]bool{}
	received := map[*Branch]*shape{}
	sentFields := map[*Branch]map[string]map[string]bool{}

	for _, run := range scenarios(records) {
		title := normal(run[0].Scenario)
		label := title
		if file := cases.file(run[0].Feature, title); file != "" {
			label = file + ": " + title
		}
		marked := slices.ContainsFunc(run, func(r trace.Record) bool { return r.Step != "" })
		acting := func(r trace.Record) bool { return !marked || r.Step == "Action" }
		last := -1
		sent := map[int][]trace.Record{} // in the order it happened
		carried := map[int]map[string]map[string]bool{}
		for i, r := range run {
			switch {
			case r.Dir == "in" && acting(r):
				last = i
				sent[i] = []trace.Record{}
				carried[i] = map[string]map[string]bool{}
				m := message(r.Kind, r.Name)
				if inputs[m] == nil {
					inputs[m] = &Input{Message: m}
					branches[m] = map[key]*Branch{}
					fields[m] = map[string]bool{}
				}
				paths(r.Request, fields[m])
			case r.Dir == "out":
				m := message(r.Kind, r.Name)
				if outputs[m] == nil {
					outputs[m] = map[string]bool{}
				}
				paths(r.Request, outputs[m])
				if last >= 0 && acting(r) {
					sent[last] = append(sent[last], r)
					if carried[last][m] == nil {
						carried[last][m] = map[string]bool{}
					}
					paths(r.Request, carried[last][m])
				}
			}
		}
		for i, r := range run {
			if _, input := sent[i]; !input {
				continue
			}
			m := message(r.Kind, r.Name)
			sends := settled(r.Threads, sent[i])
			k := key{sends: spell(sends), refusal: r.Error}
			b := branches[m][k]
			if b == nil {
				b = &Branch{Sends: sends, Refusal: k.refusal}
				branches[m][k] = b
				received[b] = &shape{}
				sentFields[b] = map[string]map[string]bool{}
			}
			if !slices.Contains(b.Cases, label) {
				b.Cases = append(b.Cases, label)
			}
			got := map[string]bool{}
			paths(r.Request, got)
			received[b].add(got)
			for sm, fs := range carried[i] {
				if sentFields[b][sm] == nil {
					sentFields[b][sm] = map[string]bool{}
				}
				maps.Copy(sentFields[b][sm], fs)
			}
		}
	}

	doc := Doc{Specrig: Format, Service: service}
	for _, m := range sorted(keys(inputs)) {
		in := *inputs[m]
		in.Fields = sorted(fields[m])
		var answered []*Branch
		for _, b := range branches[m] {
			if !b.refused() {
				answered = append(answered, b)
			}
		}
		in.Needs = needs(received, answered)
		for _, b := range branches[m] {
			slices.Sort(b.Cases)
			for sm, fs := range sentFields[b] {
				if len(fs) > 0 {
					if b.Sent == nil {
						b.Sent = map[string][]string{}
					}
					b.Sent[sm] = sorted(fs)
				}
			}
			in.Branches = append(in.Branches, *b)
		}
		slices.SortFunc(in.Branches, func(a, b Branch) int { return strings.Compare(a.anchor(in), b.anchor(in)) })
		doc.Inputs = append(doc.Inputs, in)
	}
	for _, m := range sorted(keys(outputs)) {
		doc.Outputs = append(doc.Outputs, Output{Message: m, Fields: sorted(outputs[m])})
	}
	return doc
}

type shape struct {
	every, any map[string]bool
}

func (s *shape) add(got map[string]bool) {
	if s.any == nil {
		s.every, s.any = maps.Clone(got), map[string]bool{}
	} else {
		maps.DeleteFunc(s.every, func(f string, _ bool) bool { return !got[f] })
	}
	maps.Copy(s.any, got)
}

// A need splits the answered branches exactly, or fixtures naming a unit in most
// cases would make the unit a need of the promo redeemed in some of them.
func needs(received map[*Branch]*shape, answered []*Branch) map[string][]string {
	messages := map[string]bool{}
	for _, b := range answered {
		for _, m := range b.messages() {
			messages[m] = true
		}
	}
	out := map[string][]string{}
	for m := range messages {
		var with, without []*Branch
		for _, b := range answered {
			if slices.Contains(b.messages(), m) {
				with = append(with, b)
			} else {
				without = append(without, b)
			}
		}
		if len(without) == 0 {
			continue
		}
		split := maps.Clone(received[with[0]].every)
		for _, b := range with[1:] {
			maps.DeleteFunc(split, func(f string, _ bool) bool { return !received[b].every[f] })
		}
		for _, b := range without {
			maps.DeleteFunc(split, func(f string, _ bool) bool { return received[b].any[f] })
		}
		if len(split) > 0 {
			out[m] = sorted(split)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// What an input received on the goroutine root sent, each goroutine a sequence of
// its own calls and of what it started.
func settled(root []uint64, sends []trace.Record) []Send {
	type thread struct {
		own      []trace.Record
		children map[uint64]bool
	}
	threads := map[uint64]*thread{}
	at := func(id uint64) *thread {
		if threads[id] == nil {
			threads[id] = &thread{children: map[uint64]bool{}}
		}
		return threads[id]
	}
	var top uint64
	if len(root) > 0 {
		top = root[0]
	}
	at(top)
	for _, r := range sends {
		chain := r.Threads
		if i := slices.Index(chain, top); i >= 0 {
			chain = chain[:i]
		}
		parent := top
		for j := len(chain) - 1; j >= 0; j-- {
			at(parent).children[chain[j]] = true
			parent = chain[j]
		}
		at(parent).own = append(at(parent).own, r)
	}
	first := map[uint64]int64{}
	var earliest func(id uint64) int64
	earliest = func(id uint64) int64 {
		if s, ok := first[id]; ok {
			return s
		}
		s := int64(math.MaxInt64)
		if t := threads[id]; len(t.own) > 0 {
			s = t.own[0].Seq
		}
		for c := range threads[id].children {
			s = min(s, earliest(c))
		}
		first[id] = s
		return s
	}
	var sequence func(id uint64) []Send
	sequence = func(id uint64) []Send {
		t := threads[id]
		started := make([][][]Send, len(t.own)+1)
		for c := range t.children {
			k := sort.Search(len(t.own), func(i int) bool { return t.own[i].Seq > earliest(c) })
			started[k] = append(started[k], sequence(c))
		}
		var out []Send
		for k := range started {
			out = append(out, together(started[k])...)
			if k < len(t.own) {
				out = append(out, Send{Message: message(t.own[k].Kind, t.own[k].Name)})
			}
		}
		return passes(out)
	}
	return sequence(top)
}

func passes(sends []Send) []Send {
	out := slices.Clone(sends)
	for folded := true; folded; {
		folded = false
		for size := 1; size <= len(out)/2 && !folded; size++ {
			for i := 0; i+2*size <= len(out); i++ {
				if spell(out[i:i+size]) == spell(out[i+size:i+2*size]) {
					out = slices.Delete(out, i+size, i+2*size)
					folded = true
					break
				}
			}
		}
	}
	return out
}

// Records by scenario, each in the order its calls began. A record outside any
// scenario — a boot test, a migration check — belongs to no case.
func scenarios(records []trace.Record) [][]trace.Record {
	var order []string
	by := map[string][]trace.Record{}
	for _, r := range records {
		if r.Scenario == "" {
			continue
		}
		if by[r.Scenario] == nil {
			order = append(order, r.Scenario)
		}
		by[r.Scenario] = append(by[r.Scenario], r)
	}
	out := make([][]trace.Record, 0, len(order))
	for _, s := range order {
		run := by[s]
		slices.SortStableFunc(run, func(a, b trace.Record) int { return int(a.Seq - b.Seq) })
		out = append(out, run)
	}
	return out
}

// Carries the prose of old onto fresh by anchor, the orphaned included: a failing
// run loses an anchor the passing one brings back. What finds none is orphaned.
func reconcile(old, fresh Doc) Doc {
	prose := map[string]string{}
	for _, in := range old.Inputs {
		if in.Description != "" {
			prose[in.anchor()] = in.Description
		}
		for _, b := range in.Branches {
			if b.Description != "" {
				prose[b.anchor(in)] = b.Description
			}
		}
	}
	for _, o := range old.Orphaned {
		if _, live := prose[o.Anchor]; live {
			fresh.Orphaned = append(fresh.Orphaned, o)
		} else {
			prose[o.Anchor] = o.Description
		}
	}
	used := map[string]bool{}
	for i := range fresh.Inputs {
		in := &fresh.Inputs[i]
		if d, ok := prose[in.anchor()]; ok {
			in.Description, used[in.anchor()] = d, true
		}
		for j := range in.Branches {
			b := &in.Branches[j]
			if d, ok := prose[b.anchor(*in)]; ok {
				b.Description, used[b.anchor(*in)] = d, true
			}
		}
	}
	for _, a := range sorted(keys(prose)) {
		if !used[a] {
			fresh.Orphaned = append(fresh.Orphaned, Orphan{Anchor: a, Description: prose[a]})
		}
	}
	return fresh
}

const header = "# Generated from the component suite by specrig: every whole run rewrites the\n" +
	"# structure; only the description fields are yours.\n"

func encode(v any) []byte {
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	_ = enc.Encode(v)
	return b.Bytes()
}

// Complete says whether the suite ran whole: a narrowed run saw part of the
// service, and its document would lose the rest.
func Complete() bool {
	for _, name := range []string{"test.run", "test.skip", "godog.tags"} {
		if f := flag.Lookup(name); f != nil && f.Value.String() != "" {
			return false
		}
	}
	// godog.paths is a Func flag, whose value prints empty whatever was given.
	return !slices.ContainsFunc(os.Args, func(arg string) bool {
		return strings.HasPrefix(strings.TrimLeft(arg, "-"), "godog.paths")
	})
}

// Write rewrites the document at path from what the suite traced, keeping its
// prose. The cases are read from ./feature.
func Write(path, service string, records []trace.Record) error {
	path = filepath.Join(home, path)
	fresh := build(service, records, readFeatures(filepath.Join(home, "feature")))
	var old Doc
	current, err := os.ReadFile(path)
	if err == nil {
		if err := yaml.Unmarshal(current, &old); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(fresh.Inputs) == 0 && len(old.Inputs) == 0 {
		return nil
	}
	merged := reconcile(old, fresh)
	body := append([]byte(header), encode(merged)...)
	if !bytes.Equal(current, body) {
		if err := os.WriteFile(path, body, 0o644); err != nil {
			return err
		}
	}
	if len(merged.Orphaned) > 0 {
		return fmt.Errorf("%s: %d descriptions lost their anchor — delete them or move them under the input or branch they belong to now", path, len(merged.Orphaned))
	}
	return nil
}

func keys[V any](m map[string]V) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

func sorted(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
