package flows

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/specrig/specrig-go/surface"
)

type Index struct {
	Flows    []Flow          `yaml:"flows"`
	Links    []Link          `yaml:"links,omitempty"`
	Untested []string        `yaml:"untested,omitempty"`
	Outside  []string        `yaml:"outside,omitempty"`
	System   *surface.Report `yaml:"system,omitempty"`
	Orphaned []Orphan        `yaml:"orphaned,omitempty"`
}

type Flow struct {
	Id          string   `yaml:"id"`
	Title       string   `yaml:"title,omitempty"`
	Description string   `yaml:"description,omitempty"`
	Entry       string   `yaml:"entry"`
	From        []string `yaml:"from,omitempty"`
	Steps       []string `yaml:"steps"`
}

type Link struct {
	From    string   `yaml:"from"`
	To      string   `yaml:"to"`
	Message string   `yaml:"message"`
	Notes   []string `yaml:"notes,omitempty"`
}

type service struct {
	doc      Doc
	features features
}

type step struct {
	service string
	input   Input
}

func (s step) id() string { return s.service + ": " + s.input.Message }

type project struct {
	services map[string]service
	steps    map[string]step
	consumes map[string][]string // message -> step ids
	produces map[string][]string // message -> services
	surfaces []surface.Surface   // every service's, traced or not
	names    map[string]string   // step id -> page
	foreign  []string            // a file at a record's path that is not one
}

// A service is a directory holding a record; no marker file says so.
func load(root string) (project, error) {
	p := project{services: map[string]service{}}
	err := filepath.WalkDir(root, func(dir string, d fs.DirEntry, err error) error {
		switch {
		case err != nil || !d.IsDir():
			return nil
		case dir != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "vendor" || d.Name() == "node_modules" || d.Name() == "testdata"):
			return filepath.SkipDir
		}
		s, err := surface.Read(filepath.Join(dir, surface.File))
		hasSurface, err := p.found(filepath.Join(dir, surface.File), err)
		if err != nil {
			return err
		}
		doc, err := readDoc(filepath.Join(dir, "tests", "flows.yaml"))
		hasDoc, err := p.found(filepath.Join(dir, "tests", "flows.yaml"), err)
		if err != nil {
			return err
		}
		if !hasSurface && !hasDoc {
			return nil
		}
		if hasSurface {
			cfg, err := surface.LoadConfig(dir)
			if err != nil {
				return err
			}
			s.Hosts = cfg.Clients
			p.surfaces = append(p.surfaces, s)
		}
		if hasDoc {
			p.services[doc.Service] = service{doc: doc, features: readFeatures(filepath.Join(dir, "tests", "feature"))}
		}
		return filepath.SkipDir
	})
	if err != nil {
		return p, err
	}
	p.index()
	return p, nil
}

// A file at a record's path that is not one is somebody else's: named, not failed on.
func (p *project) found(path string, err error) (bool, error) {
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	case errors.Is(err, surface.ErrForeign):
		p.foreign = append(p.foreign, path)
		return false, nil
	}
	return false, err
}

func readDoc(path string) (Doc, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return Doc{}, err
	}
	if err := surface.Identify(body, Format); err != nil {
		return Doc{}, fmt.Errorf("%s: %w", path, err)
	}
	var doc Doc
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return Doc{}, fmt.Errorf("%s: %w", path, err)
	}
	if doc.Service == "" {
		return Doc{}, fmt.Errorf("%s: no service", path)
	}
	return doc, nil
}

func (p *project) index() {
	p.steps, p.consumes, p.produces = map[string]step{}, map[string][]string{}, map[string][]string{}
	for _, name := range sorted(keys(p.services)) {
		doc := p.services[name].doc
		for _, in := range doc.Inputs {
			s := step{service: name, input: in}
			p.steps[s.id()] = s
			p.consumes[in.Message] = append(p.consumes[in.Message], s.id())
		}
		for _, out := range doc.Outputs {
			p.produces[out.Message] = append(p.produces[out.Message], name)
		}
	}
	p.names = names(p.steps)
}

func kind(m string) string { k, _, _ := strings.Cut(m, " "); return k }

// Where a message goes that no traced case followed, by the code of the whole set.
func (p project) beyond(from, m string) string {
	if len(p.surfaces) == 0 {
		return "not traced"
	}
	k, name, _ := strings.Cut(m, " ")
	exact, wild := surface.Receivers(p.surfaces, from, k, name)
	if k != "event" {
		exact = slices.DeleteFunc(exact, func(s string) bool { return s == from })
	}
	switch {
	case len(exact) > 0:
		return "received by " + strings.Join(exact, ", ") + ", no case took it"
	case len(wild) > 0:
		return "only a wildcard subscription: " + strings.Join(wild, ", ")
	}
	return "out of the system"
}

func (p project) senders(s step) []string {
	k, name, _ := strings.Cut(s.input.Message, " ")
	return slices.DeleteFunc(surface.Senders(p.surfaces, s.service, k, name), func(from string) bool {
		return from == s.service && k != "event"
	})
}

func (p project) untested(traced []Link) []string {
	had := map[string]bool{}
	for _, l := range traced {
		had[l.From+" → "+l.To+": "+l.Message] = true
	}
	var out []string
	add := func(from, to, m string) {
		if _, ok := p.services[to]; ok && !had[from+" → "+to+": "+m] {
			out = append(out, from+" → "+to+": "+m)
		}
	}
	for _, s := range p.surfaces {
		if _, ok := p.services[s.Service]; !ok {
			continue
		}
		for _, key := range s.Publishes {
			exact, _ := surface.Receivers(p.surfaces, s.Service, "event", key)
			for _, to := range exact {
				add(s.Service, to, "event "+key)
			}
		}
		for _, m := range s.Calls {
			if !strings.HasPrefix(m, "/") {
				continue // outgoing HTTP is not traced, so no case could follow it
			}
			exact, _ := surface.Receivers(p.surfaces, s.Service, "grpc", m)
			for _, to := range exact {
				if to != s.Service {
					add(s.Service, to, "grpc "+m)
				}
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// A call goes to another service; an event may come back to the one that sent
// it, since a service consumes its own events through the bus like anyone's.
func (p project) receivers(from, m string) []string {
	var out []string
	for _, id := range p.consumes[m] {
		if p.steps[id].service != from || kind(m) == "event" {
			out = append(out, id)
		}
	}
	return out
}

func (p project) entry(s step) bool {
	switch kind(s.input.Message) {
	case "http", "command":
		return true
	}
	for _, from := range p.produces[s.input.Message] {
		if from != s.service || kind(s.input.Message) == "event" {
			return false
		}
	}
	return true
}

// Everything reachable from the entry through the branches the flow takes,
// depth first: a step comes right after the first message that reaches it, the
// way the diagram draws it. onward says whether anything did.
func (p project) walk(entry string) (order []string, onward bool) {
	r := p.route(entry)
	return r.order, r.onward
}

type route struct {
	entry   string
	order   []string
	onward  bool
	carried map[string]map[string]bool // step id -> the fields of what reaches it
}

// What reaches a step depends on the branches its senders take, which depends
// on what reaches them: the walk repeats until that stops growing.
func (p project) route(entry string) route {
	carried := map[string]map[string]bool{}
	for {
		r := route{entry: entry, carried: carried}
		next := map[string]map[string]bool{}
		seen := map[string]bool{}
		var visit func(id string)
		visit = func(id string) {
			seen[id] = true
			r.order = append(r.order, id)
			s := p.steps[id]
			taken, _ := r.branches(s, id)
			for _, sent := range sequence(taken) {
				for _, to := range p.receivers(s.service, sent.message) {
					r.onward = true
					if next[to] == nil {
						next[to] = map[string]bool{}
					}
					for _, b := range taken {
						for _, f := range b.Sent[sent.message] {
							next[to][f] = true
						}
					}
					if !seen[to] {
						visit(to)
					}
				}
			}
		}
		visit(entry)
		if maps.EqualFunc(next, carried, maps.Equal) {
			return r
		}
		carried = next
	}
}

type folded struct {
	message string
	missing []string
}

// The entry is taken whole: nothing that reaches it is known.
func (r route) branches(s step, id string) ([]Branch, []folded) {
	if id == r.entry {
		return s.input.Branches, nil
	}
	var by []folded
	for _, m := range sorted(keys(s.input.Needs)) {
		if missing := slices.DeleteFunc(slices.Clone(s.input.Needs[m]), func(f string) bool { return r.carried[id][f] }); len(missing) > 0 {
			by = append(by, folded{message: m, missing: missing})
		}
	}
	if len(by) == 0 {
		return s.input.Branches, nil
	}
	isFolded := func(m string) bool { return slices.ContainsFunc(by, func(f folded) bool { return f.message == m }) }
	whole := map[string]bool{}
	for _, b := range s.input.Branches {
		if !slices.ContainsFunc(b.Sends, isFolded) {
			for _, m := range b.Sends {
				whole[m] = true
			}
		}
	}
	var out []Branch
	for _, b := range s.input.Branches {
		lost := slices.ContainsFunc(b.Sends, isFolded)
		b.Sends = slices.DeleteFunc(slices.Clone(b.Sends), isFolded)
		// Its cases sent what this flow cannot; it stays only for what no other branch sends.
		if lost && !b.refused() && !slices.ContainsFunc(b.Sends, func(m string) bool { return !whole[m] }) {
			continue
		}
		out = append(out, b)
	}
	return out, by
}

// A message as a branch sends it: the nth time that branch sends it.
type send struct {
	message string
	nth     int
}

func sends(messages []string) []send {
	seen := map[string]int{}
	out := make([]send, 0, len(messages))
	for _, m := range messages {
		seen[m]++
		out = append(out, send{message: m, nth: seen[m]})
	}
	return out
}

// A before B when some case sent A first; ties and conflicts go alphabetically.
func sequence(branches []Branch) []send {
	after := map[send]map[send]bool{}
	before := map[send]int{}
	nodes := map[send]bool{}
	for _, b := range branches {
		if b.refused() {
			continue
		}
		order := sends(b.Sends)
		for i, s := range order {
			nodes[s] = true
			for _, later := range order[i+1:] {
				if after[s] == nil {
					after[s] = map[send]bool{}
				}
				if !after[s][later] {
					after[s][later] = true
					before[later]++
				}
			}
		}
	}
	left := make([]send, 0, len(nodes))
	for s := range nodes {
		left = append(left, s)
	}
	slices.SortFunc(left, func(a, b send) int {
		if c := strings.Compare(a.message, b.message); c != 0 {
			return c
		}
		return a.nth - b.nth
	})
	var out []send
	for len(left) > 0 {
		pick := slices.IndexFunc(left, func(s send) bool { return before[s] == 0 })
		if pick < 0 {
			pick = 0
		}
		s := left[pick]
		left = slices.Delete(left, pick, pick+1)
		out = append(out, s)
		for later := range after[s] {
			before[later]--
		}
	}
	return out
}

var unsafe = regexp.MustCompile(`[^a-z0-9]+`)

var cyrillic = strings.NewReplacer(
	"а", "a", "б", "b", "в", "v", "г", "g", "д", "d", "е", "e", "ё", "e", "ж", "zh", "з", "z", "и", "i", "й", "y",
	"к", "k", "л", "l", "м", "m", "н", "n", "о", "o", "п", "p", "р", "r", "с", "s", "т", "t", "у", "u", "ф", "f",
	"х", "kh", "ц", "ts", "ч", "ch", "ш", "sh", "щ", "shch", "ъ", "", "ы", "y", "ь", "", "э", "e", "ю", "yu", "я", "ya",
)

// A page is named by its step, which stays put; a title is prose and may change.
func (p project) slug(s step) string { return p.names[s.id()] }

// A hash, not a counter: a renamed page loses its prose to orphaned, never to a neighbour.
func names(steps map[string]step) map[string]string {
	out := make(map[string]string, len(steps))
	for name, ids := range named(steps, slices.Collect(maps.Keys(steps)), short) {
		if len(ids) == 1 {
			out[ids[0]] = name
			continue
		}
		for full, same := range named(steps, ids, whole) {
			for _, id := range same {
				out[id] = full
				if len(same) > 1 {
					out[id] = fmt.Sprintf("%s-%x", full, sha256.Sum256([]byte(id)))[:len(full)+7]
				}
			}
		}
	}
	return out
}

func named(steps map[string]step, ids []string, of func(string) string) map[string][]string {
	out := map[string][]string{}
	for _, id := range ids {
		s := steps[id]
		name := strings.Trim(unsafe.ReplaceAllString(cyrillic.Replace(strings.ToLower(s.service+"-"+kind(s.input.Message)+"-"+of(s.input.Message))), "-"), "-")
		out[name] = append(out[name], id)
	}
	return out
}

func whole(m string) string { _, name, _ := strings.Cut(m, " "); return name }

// The name of a message without its package: Service.Method for a call.
func short(m string) string {
	k, name, _ := strings.Cut(m, " ")
	if k != "grpc" {
		return name
	}
	parts := strings.Split(strings.TrimPrefix(name, "/"), "/")
	if len(parts) != 2 {
		return name
	}
	return parts[0][strings.LastIndex(parts[0], ".")+1:] + "." + parts[1]
}

const pageHeader = "<!-- Generated by `go tool specrig flows` from the services' tests/flows.yaml. Do not edit:\n" +
	"     prose goes into the description fields of those files and of the index. -->\n"

// Generated says whether a page is one Assemble writes.
func Generated(page []byte) bool {
	return bytes.HasPrefix(page, []byte(pageHeader))
}

// Assemble builds the index at path and a page per input a flow reaches beside
// it, keeping the prose the index at path already has. Foreign are the files at
// a record's path it took for somebody else's.
func Assemble(root, path string) (files map[string][]byte, foreign []string, err error) {
	p, err := load(root)
	if err != nil {
		return nil, nil, err
	}
	if len(p.services) == 0 && len(p.surfaces) == 0 {
		return nil, nil, fmt.Errorf("no service below %s has tests/flows.yaml or %s: run its suite, or `go tool specrig surface`", root, surface.File)
	}
	var old Index
	if body, err := os.ReadFile(path); err == nil {
		if err := yaml.Unmarshal(body, &old); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	prose := map[string]Flow{}
	for _, f := range old.Flows {
		if f.Title != "" || f.Description != "" {
			prose[f.Id] = f
		}
	}

	files = map[string][]byte{}
	index := Index{}
	for _, o := range old.Orphaned {
		id := strings.TrimPrefix(o.Anchor, "flow ")
		if _, live := prose[id]; live {
			index.Orphaned = append(index.Orphaned, o)
		} else {
			prose[id] = Flow{Id: id, Title: o.Title, Description: o.Description}
		}
	}
	used := map[string]bool{}
	reached := map[string]bool{}
	for _, id := range sorted(keys(p.steps)) {
		s := p.steps[id]
		if !p.entry(s) {
			continue
		}
		order, onward := p.walk(id)
		if !onward {
			continue
		}
		f := Flow{Id: p.slug(s), Entry: id, From: p.senders(s), Steps: order}
		if was, ok := prose[f.Id]; ok {
			f.Title, f.Description, used[f.Id] = was.Title, was.Description, true
		}
		index.Flows = append(index.Flows, f)
		files[filepath.Join(filepath.Dir(path), f.Id+".md")] = page(p, f, true)
		for _, step := range order[1:] {
			reached[step] = true
		}
	}
	for _, id := range sorted(reached) {
		order, _ := p.walk(id)
		f := Flow{Id: p.slug(p.steps[id]), Entry: id, Steps: order}
		files[filepath.Join(filepath.Dir(path), f.Id+".md")] = page(p, f, false)
	}
	for _, id := range sorted(keys(prose)) {
		if !used[id] {
			index.Orphaned = append(index.Orphaned, Orphan{Anchor: "flow " + id, Title: prose[id].Title, Description: prose[id].Description})
		}
	}
	index.Links, index.Outside = p.links()
	index.Untested = p.untested(index.Links)
	if len(p.surfaces) > 0 {
		report, err := surface.Verify(p.surfaces)
		if err != nil {
			return nil, nil, err
		}
		index.System = &report
	}
	files[path] = append([]byte("# Generated by `go tool specrig flows`. Only the title and description of a flow are yours.\n"), encode(index)...)
	return files, p.foreign, nil
}

func (p project) links() ([]Link, []string) {
	var links []Link
	var outside []string
	for _, from := range sorted(keys(p.services)) {
		for _, out := range p.services[from].doc.Outputs {
			receivers := p.receivers(from, out.Message)
			if len(receivers) == 0 {
				outside = append(outside, from+" sends "+out.Message+": "+p.beyond(from, out.Message))
			}
			sent := set(out.Fields)
			for _, to := range receivers {
				l := Link{From: from, To: p.steps[to].service, Message: out.Message}
				if extra := missing(set(p.steps[to].input.Fields), sent); len(extra) > 0 {
					l.Notes = append(l.Notes, "the receiver's cases send fields the sender never does: "+strings.Join(extra, ", "))
				}
				links = append(links, l)
			}
		}
	}
	return links, outside
}

// LinkChanges says, between two versions of an index, which links between
// services were lost and which appeared: the reason a page changed.
func LinkChanges(was, now []byte) []string {
	var before, after Index
	if yaml.Unmarshal(was, &before) != nil || yaml.Unmarshal(now, &after) != nil {
		return nil
	}
	name := func(l Link) string { return fmt.Sprintf("%s → %s: %s", l.From, l.To, l.Message) }
	had, has := map[string]bool{}, map[string]bool{}
	for _, l := range before.Links {
		had[name(l)] = true
	}
	for _, l := range after.Links {
		has[name(l)] = true
	}
	var out []string
	for _, l := range sorted(had) {
		if !has[l] {
			out = append(out, "link lost: "+l)
		}
	}
	for _, l := range sorted(has) {
		if !had[l] {
			out = append(out, "link new: "+l)
		}
	}
	return out
}

// Paths one side relies on and the other never shows. A scalar where the other
// side has an object is a case feeding a broken message, not a field.
func missing(has, lacks map[string]bool) []string {
	var out []string
	for _, pth := range sorted(has) {
		if lacks[pth] {
			continue
		}
		broken := false
		for q := range lacks {
			if strings.HasPrefix(q, pth+".") || strings.HasPrefix(q, pth+"[]") {
				broken = true
				break
			}
		}
		if !broken {
			out = append(out, pth)
		}
	}
	return out
}

func set(list []string) map[string]bool {
	out := make(map[string]bool, len(list))
	for _, s := range list {
		out[s] = true
	}
	return out
}
