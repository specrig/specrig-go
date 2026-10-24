package flows

import (
	"fmt"
	"slices"
	"strings"
)

func page(p project, f Flow, entry bool) []byte {
	var b strings.Builder
	b.WriteString(pageHeader)
	title := f.Title
	if title == "" {
		title = f.Entry
	}
	fmt.Fprintf(&b, "\n# %s\n\n", title)
	if f.Description != "" {
		fmt.Fprintf(&b, "%s\n\n", f.Description)
	}
	origin, senders := p.origin(f, entry)
	fmt.Fprintf(&b, "Entry: `%s` — %s\n\n", f.Entry, origin)
	if len(senders) > 0 {
		fmt.Fprintf(&b, "<details>\n<summary>Received from (%d)</summary>\n\n", len(senders))
		for _, s := range senders {
			fmt.Fprintf(&b, "- %s\n", s)
		}
		b.WriteString("\n</details>\n\n")
	}

	number := map[string]int{}
	for i, id := range f.Steps {
		number[id] = i + 1
	}
	r := p.route(f.Entry)
	d := diagram{p: p, r: r, b: &b, number: number, drawn: map[string]bool{}}
	b.WriteString("```mermaid\nsequenceDiagram\n")
	b.WriteString("  participant ext_in as in\n")
	var services []string
	for _, id := range f.Steps {
		if svc := p.steps[id].service; !slices.Contains(services, svc) {
			services = append(services, svc)
			fmt.Fprintf(&b, "  participant %s as %s\n", participant(svc), svc)
		}
	}
	first := p.steps[f.Entry]
	fmt.Fprintf(&b, "  ext_in%s%s: 1 · %s\n", arrow(first.input.Message), participant(first.service), sequenceText(short(first.input.Message)))
	d.from(f.Entry, 0)
	b.WriteString("```\n\nA solid arrow is a call that waits for its answer; an open one is an event over the bus. " +
		"`in` is where the flow comes from. An arrow carries the number of the step it reaches, so two arrows into one step share it; " +
		"a block holds what a step sends only in some of its cases. " +
		"A case is a scenario in one branch of a step: a scenario that passed the step twice by different paths is two cases.\n")

	for i, id := range f.Steps {
		s := p.steps[id]
		fmt.Fprintf(&b, "\n## %d. %s · %s\n\n", i+1, s.service, short(s.input.Message))
		fmt.Fprintf(&b, "`%s`\n\n", s.input.Message)
		if s.input.Description != "" {
			fmt.Fprintf(&b, "%s\n\n", s.input.Description)
		}
		if i == 0 {
			p.cases(&b, s)
			continue
		}
		for _, br := range s.input.Branches {
			fmt.Fprintf(&b, "- %s — %s\n", p.branchName(s, br), count(len(br.Cases)))
		}
		_, by := r.branches(s, id)
		for _, f := range by {
			fmt.Fprintf(&b, "- not followed here: %s — %s\n", p.branchName(s, Branch{Sends: []string{f.message}}), needing(f.missing))
		}
		fmt.Fprintf(&b, "\nIts cases: [%s](%s.md)\n", p.slug(s), p.slug(s))
	}
	return []byte(b.String())
}

// The senders come back apart when there are more than a line holds.
func (p project) origin(f Flow, entry bool) (string, []string) {
	s := p.steps[f.Entry]
	if !entry {
		var from []string
		for _, id := range sorted(keys(p.steps)) {
			sender := p.steps[id]
			for _, sent := range sequence(sender.input.Branches) {
				if sent.message == s.input.Message && slices.Contains(p.receivers(sender.service, sent.message), f.Entry) {
					from = append(from, fmt.Sprintf("[%s](%s.md)", id, p.slug(sender)))
					break
				}
			}
		}
		if len(from) > 3 {
			return fmt.Sprintf("received from %d steps", len(from)), from
		}
		return "received from " + strings.Join(from, ", "), nil
	}
	switch {
	case len(f.From) > 0:
		return "called in code by " + strings.Join(f.From, ", ") + ", which has no trace", nil
	case len(p.surfaces) > 0 && (kind(s.input.Message) == "grpc" || kind(s.input.Message) == "event"):
		// RPCs and events stay inside the set: one nobody sends is dead or called from a party not in it.
		return "nothing in code calls it", nil
	}
	return "from outside the system", nil
}

func (p project) cases(b *strings.Builder, s step) {
	svc := p.services[s.service]
	var onward, rest []Branch
	for _, br := range s.input.Branches {
		if !br.refused() && slices.ContainsFunc(br.Sends, func(m string) bool { return len(p.receivers(s.service, m)) > 0 }) {
			onward = append(onward, br)
		} else {
			rest = append(rest, br)
		}
	}
	for _, br := range onward {
		p.branch(b, s, svc, br, "###")
	}
	if len(rest) > 0 {
		fmt.Fprintf(b, "<details>\n<summary>Other branches of the step (%d)</summary>\n\n", len(rest))
		for _, br := range rest {
			p.branch(b, s, svc, br, "####")
		}
		b.WriteString("</details>\n")
	}
}

func (p project) branchName(s step, br Branch) string {
	if br.refused() {
		return "refused: " + br.Refusal
	}
	var to []string
	for _, m := range br.Sends {
		if receivers := p.receivers(s.service, m); len(receivers) > 0 {
			var where []string
			for _, r := range receivers {
				where = append(where, p.steps[r].service)
			}
			to = append(to, fmt.Sprintf("%s → %s", short(m), strings.Join(where, ", ")))
		} else {
			to = append(to, short(m)+" ("+p.beyond(s.service, m)+")")
		}
	}
	if len(to) == 0 {
		return "nothing further"
	}
	return strings.Join(to, "; ")
}

func (p project) branch(b *strings.Builder, s step, svc service, br Branch, level string) {
	fmt.Fprintf(b, "%s %s\n\n", level, p.branchName(s, br))
	if br.Description != "" {
		fmt.Fprintf(b, "%s\n\n", br.Description)
	}
	for _, c := range br.Cases {
		file, title, found := strings.Cut(c, ": ")
		if !found || !strings.HasSuffix(file, ".feature") {
			file, title = "", c
		}
		fmt.Fprintf(b, "- **%s**", title)
		if file != "" {
			fmt.Fprintf(b, " · `%s`", file)
		}
		b.WriteString("\n")
		if comment := svc.features.comments[caseKey{file, title}]; comment != "" {
			for _, par := range strings.Split(comment, "\n\n") {
				fmt.Fprintf(b, "\n  %s\n", par)
			}
		}
		b.WriteString("\n")
	}
}

func count(n int) string {
	if n == 1 {
		return "1 case"
	}
	return fmt.Sprintf("%d cases", n)
}

func needing(fields []string) string {
	return "sent only in cases that carried " + strings.Join(fields, ", ") + ", which nothing reaching it carries"
}

type diagram struct {
	p      project
	r      route
	b      *strings.Builder
	number map[string]int
	drawn  map[string]bool
}

func (d *diagram) from(id string, depth int) {
	d.drawn[id] = true
	s := d.p.steps[id]
	in := s.input
	var by []folded
	in.Branches, by = d.r.branches(s, id)
	type arrowTo struct {
		send
		to string
	}
	var arrows []arrowTo
	for _, sent := range sequence(in.Branches) {
		for _, to := range d.p.receivers(s.service, sent.message) {
			arrows = append(arrows, arrowTo{send: sent, to: to})
		}
	}
	// A message sent in exactly the branches of the open block joins it; one sent
	// in only some of them opens a block inside it.
	var open [][]int
	pad := func() string { return strings.Repeat("  ", depth+len(open)+1) }
	push := func(who []int) {
		var of []int
		if len(open) > 0 {
			of = open[len(open)-1]
		}
		fmt.Fprintf(d.b, "%sopt %s\n", pad(), sequenceText(condition(in, who, of, d.number[id])))
		open = append(open, who)
	}
	for i, e := range arrows {
		who, sometimes := sending(in, e.send)
		for len(open) > 0 && (!sometimes || !within(who, open[len(open)-1])) {
			open = open[:len(open)-1]
			fmt.Fprintf(d.b, "%send\n", pad())
		}
		if sometimes && (len(open) == 0 || !slices.Equal(open[len(open)-1], who)) {
			// A wider condition sent later in the step opens first, so this one
			// can sit inside it.
			var wider [][]int
			for _, next := range arrows[i+1:] {
				nw, ns := sending(in, next.send)
				if !ns || !within(who, nw) || len(open) > 0 && !within(nw, open[len(open)-1]) {
					break
				}
				if !slices.Equal(nw, who) && !slices.ContainsFunc(wider, func(w []int) bool { return slices.Equal(w, nw) }) {
					wider = append(wider, nw)
				}
			}
			slices.SortStableFunc(wider, func(a, b []int) int { return len(b) - len(a) })
			for _, w := range wider {
				if len(open) == 0 || within(w, open[len(open)-1]) {
					push(w)
				}
			}
			push(who)
		}
		to := d.p.steps[e.to]
		fmt.Fprintf(d.b, "%s%s%s%s: %d · %s\n", pad(), participant(s.service), arrow(e.message),
			participant(to.service), d.number[e.to], sequenceText(short(e.message)))
		if to.service == s.service {
			fmt.Fprintf(d.b, "%sNote over %s: its own event, back through the bus\n", pad(), participant(s.service))
		}
		if !d.drawn[e.to] {
			d.from(e.to, depth+len(open))
		}
	}
	for len(open) > 0 {
		open = open[:len(open)-1]
		fmt.Fprintf(d.b, "%send\n", pad())
	}
	for _, f := range by {
		fmt.Fprintf(d.b, "%sNote over %s: not followed: %s — %s\n", pad(), participant(s.service),
			sequenceText(d.p.branchName(s, Branch{Sends: []string{f.message}})), sequenceText(needing(f.missing)))
	}
}

// The step's branches that make this send, the refused ones aside, and whether
// some answered branch does not.
func sending(in Input, s send) ([]int, bool) {
	var who []int
	answered := 0
	for i, b := range in.Branches {
		if b.refused() {
			continue
		}
		answered++
		if slices.Contains(sends(b.Sends), s) {
			who = append(who, i)
		}
	}
	return who, len(who) < answered
}

func within(inner, outer []int) bool {
	for _, i := range inner {
		if !slices.Contains(outer, i) {
			return false
		}
	}
	return true
}

// What people wrote on the branches, or, where they wrote nothing, in how many
// cases: of the step's, or of the enclosing block's when there is one.
func condition(in Input, who, of []int, step int) string {
	var prose []string
	for _, i := range who {
		if in.Branches[i].Description == "" {
			prose = nil
			break
		}
		prose = append(prose, strings.Join(strings.Fields(in.Branches[i].Description), " "))
	}
	if len(prose) > 0 {
		return strings.Join(prose, "; or ")
	}
	if of != nil {
		return fmt.Sprintf("in %d of these %d", cases(in, who), cases(in, of))
	}
	var answered []int
	for i, b := range in.Branches {
		if !b.refused() {
			answered = append(answered, i)
		}
	}
	return fmt.Sprintf("in %d of %d cases of step %d", cases(in, who), cases(in, answered), step)
}

func cases(in Input, branches []int) int {
	n := 0
	for _, i := range branches {
		n += len(in.Branches[i].Cases)
	}
	return n
}

func arrow(m string) string {
	if kind(m) == "event" {
		return "-)"
	}
	return "->>"
}

var unsafeParticipant = strings.NewReplacer("-", "_", ".", "_", " ", "_")

func participant(service string) string { return "svc_" + unsafeParticipant.Replace(service) }

// Mermaid ends a message at a semicolon and reads # as an entity.
func sequenceText(s string) string {
	return strings.NewReplacer("#", "#35;", ";", "#59;").Replace(s)
}
