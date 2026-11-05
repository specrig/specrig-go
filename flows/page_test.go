package flows

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/specrig/specrig-go/trace"
)

func projectOf(steps ...step) project {
	p := project{services: map[string]service{}}
	for _, s := range steps {
		doc := p.services[s.service].doc
		doc.Inputs = append(doc.Inputs, s.input)
		for _, b := range s.input.Branches {
			for _, m := range b.Sends {
				doc.Outputs = append(doc.Outputs, Output{Message: m})
			}
		}
		p.services[s.service] = service{doc: doc}
	}
	p.index()
	return p
}

func in(service, message string, branches ...Branch) step {
	return step{service: service, input: Input{Message: message, Branches: branches}}
}

// The gateway charges only when the cart is not free; what the charge sends on
// happens only then, so it belongs inside the block, not after it.
func TestWhatAStepSendsSitsUnderTheConditionThatReachedIt(t *testing.T) {
	p := projectOf(
		in("gateway", "http POST /pay",
			Branch{Sends: []string{"grpc /pay.Pay/Charge"}, Cases: []string{"a", "b"}},
			Branch{Cases: []string{"c"}}),
		in("pay", "grpc /pay.Pay/Charge",
			Branch{Sends: []string{"event paid"}, Cases: []string{"d"}}),
		in("books", "event paid", Branch{Cases: []string{"e"}}),
	)
	order, _ := p.walk("gateway: http POST /pay")
	body := string(page(p, Flow{Id: "gateway-http-post-pay", Entry: "gateway: http POST /pay", Steps: order}, true))

	charge := strings.Index(body, "svc_gateway->>svc_pay")
	paid := strings.Index(body, "svc_pay-)svc_books")
	end := strings.Index(body, "end\n")
	if charge < 0 || paid < 0 || end < 0 {
		t.Fatalf("an arrow or the block is missing:\n%s", body)
	}
	if charge >= paid || paid >= end {
		t.Errorf("what the charge sends is outside the block that made the charge:\n%s", body)
	}
}

func TestAStepFurtherOnLinksToItsOwnPageInsteadOfRepeatingItsCases(t *testing.T) {
	p := projectOf(
		in("gateway", "http POST /pay", Branch{Sends: []string{"grpc /pay.Pay/Charge"}, Cases: []string{"x.feature: Paying"}}),
		in("pay", "grpc /pay.Pay/Charge",
			Branch{Cases: []string{"pay.feature: A charge goes through"}},
			Branch{Refusal: "NotFound method_not_found", Cases: []string{"pay.feature: An unknown method"}}),
	)
	order, _ := p.walk("gateway: http POST /pay")
	body := string(page(p, Flow{Id: "gateway-http-post-pay", Entry: "gateway: http POST /pay", Steps: order}, true))

	for _, want := range []string{
		"- nothing further — 1 case",
		"- refused: NotFound method_not_found — 1 case",
		"Its cases: [pay-grpc-pay-charge](pay-grpc-pay-charge.md)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "A charge goes through") {
		t.Errorf("the page repeats the cases of a step further on:\n%s", body)
	}
}

func TestAStepManySendReachListsThemFolded(t *testing.T) {
	var steps []step
	for _, route := range []string{"/a", "/b", "/c", "/d"} {
		steps = append(steps, in("gateway", "http GET "+route, Branch{Sends: []string{"grpc /auth.Auth/Summon"}, Cases: []string{"x"}}))
	}
	p := projectOf(append(steps, in("auth", "grpc /auth.Auth/Summon", Branch{Cases: []string{"y"}}))...)
	body := string(page(p, Flow{Id: "auth-grpc-auth-summon", Entry: "auth: grpc /auth.Auth/Summon", Steps: []string{"auth: grpc /auth.Auth/Summon"}}, false))

	for _, want := range []string{
		"— received from 4 steps\n",
		"<summary>Received from (4)</summary>",
		"- [gateway: http GET /d](gateway-http-get-d.md)\n",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page lacks %q:\n%s", want, body)
		}
	}
}

func TestALoopIsOnePassAndAReturnIsAStep(t *testing.T) {
	for _, c := range []struct{ sent, want []string }{
		{[]string{"a", "a", "a"}, []string{"a"}},
		{[]string{"a", "b", "a", "b"}, []string{"a", "b"}},
		{[]string{"otp", "render", "store", "otp"}, []string{"otp", "render", "store", "otp"}},
		{[]string{"x", "a", "b", "a", "b", "y"}, []string{"x", "a", "b", "y"}},
	} {
		if got := passes(c.sent); !slices.Equal(got, c.want) {
			t.Errorf("passes(%v) = %v, want %v", c.sent, got, c.want)
		}
	}
}

// A link asked for, refused, the document drawn and stored, the link asked for
// again: the second ask is an arrow of its own, after the store.
func TestASendRepeatedAfterOthersIsDrawnWhereItHappened(t *testing.T) {
	p := projectOf(
		in("herald", "grpc /doc.Docs/Link", Branch{
			Sends: []string{"grpc /media.Media/Link", "grpc /media.Media/Store", "grpc /media.Media/Link"},
			Cases: []string{"a"},
		}),
		in("media", "grpc /media.Media/Link", Branch{Cases: []string{"b"}}),
		in("media", "grpc /media.Media/Store", Branch{Cases: []string{"c"}}),
	)
	order, _ := p.walk("herald: grpc /doc.Docs/Link")
	body := string(page(p, Flow{Id: "herald-grpc-docs-link", Entry: "herald: grpc /doc.Docs/Link", Steps: order}, true))

	first := strings.Index(body, "Media.Link")
	store := strings.Index(body, "Media.Store")
	again := strings.LastIndex(body, "Media.Link")
	if first >= store || store >= again {
		t.Errorf("the second link is not drawn after the store:\n%s", body)
	}
}

func TestAnEventBackToItsOwnSenderIsMarked(t *testing.T) {
	p := projectOf(
		in("rent", "command renew", Branch{Sends: []string{"event paid"}, Cases: []string{"a"}}),
		in("rent", "event paid", Branch{Cases: []string{"b"}}),
	)
	order, _ := p.walk("rent: command renew")
	body := string(page(p, Flow{Id: "rent-command-renew", Entry: "rent: command renew", Steps: order}, true))
	if !strings.Contains(body, "Note over svc_rent: its own event, back through the bus") {
		t.Errorf("the loop is not marked:\n%s", body)
	}
}

// Two files may hold a case of one title; the file the suite ran it from is the
// one its label names, whichever of them sorts last.
func TestACaseIsLabelledByTheFileItRanFrom(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"contract.feature": "Feature: Contracts\n\n  Scenario: An admin without the permission is forbidden\n    When it is sent\n",
		"price.feature":    "Feature: Prices\n\n  Scenario: An admin without the permission is forbidden\n    When it is sent\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	records := []trace.Record{{
		Seq: 1, Scenario: "TestFeatures/An_admin_without_the_permission_is_forbidden",
		Feature: filepath.Join(dir, "contract.feature"), Dir: "in", Kind: "http", Name: "DELETE /contracts/{id}", Error: "HTTP 403 forbidden",
	}}

	doc := build("gateway", records, readFeatures(dir))

	got := doc.Inputs[0].Branches[0].Cases
	if len(got) != 1 || got[0] != "contract.feature: An admin without the permission is forbidden" {
		t.Errorf("cases %v, want the one of contract.feature", got)
	}
}

// godog runs an outline once per Examples row, under its title filled from the row.
func TestAnOutlineRowIsLabelledByTheFileItRanFrom(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "tax.feature"), "Feature: Tax\n\n"+
		"  Scenario Outline: A line fails the calculation: <case>\n    When a line of <category> is priced\n\n"+
		"    Examples:\n      | case                             | category |\n      | a category the law does not know | goods    |\n")
	records := []trace.Record{{
		Seq: 1, Scenario: "TestFeatures/A_line_fails_the_calculation:_a_category_the_law_does_not_know",
		Feature: filepath.Join(dir, "tax.feature"), Dir: "in", Kind: "grpc", Name: "/tax.Tax/Calculate", Error: "rpc error: code = InvalidArgument",
	}}

	doc := build("shark", records, readFeatures(dir))

	got := doc.Inputs[0].Branches[0].Cases
	if len(got) != 1 || got[0] != "tax.feature: A line fails the calculation: a category the law does not know" {
		t.Errorf("cases %v, want the one of tax.feature", got)
	}
}

func pageOfCases(t *testing.T, feature string, branches ...[]string) string {
	t.Helper()
	root := t.TempDir()
	input := Input{Message: "http POST /orders"}
	for i, titles := range branches {
		b := Branch{Sends: []string{"grpc /stock.S/Reserve"}}
		if i > 0 {
			b = Branch{Refusal: fmt.Sprintf("HTTP 40%d", i)}
		}
		for _, title := range titles {
			b.Cases = append(b.Cases, "a.feature: "+title)
		}
		input.Branches = append(input.Branches, b)
	}
	writeFile(t, filepath.Join(root, "stock", "tests", "flows.yaml"), stock)
	writeFile(t, filepath.Join(root, "shop", "tests", "features", "a.feature"), feature)
	writeFile(t, filepath.Join(root, "shop", "tests", "flows.yaml"), string(encode(Doc{Specrig: Format, Service: "shop", Inputs: []Input{input}})))

	files, _, err := Assemble(root, filepath.Join(root, "docs", "flows.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(files[filepath.Join(root, "docs", "shop-http-post-orders.md")])
}

func TestARuleStandsOnceAboveItsCasesWithoutItsBackground(t *testing.T) {
	body := pageOfCases(t, `Feature: Orders

  Rule: Stock is held before the order is taken

    A held unit cannot be sold twice.
    And nobody else sees it.

    Then the next buyer waits.

    Background: The warehouse
      About the warehouse, not the rule.

      Given the warehouse is open

    Scenario: Placing a book
      When it is sent

    Scenario: Placing a pen
      When it is sent
`, []string{"Placing a book", "Placing a pen"})

	want := "#### Rule: Stock is held before the order is taken\n\n" +
		"A held unit cannot be sold twice. And nobody else sees it.\n\nThen the next buyer waits.\n\n" +
		"- **Placing a book** · `a.feature`\n\n" +
		"- **Placing a pen** · `a.feature`\n\n"
	if !strings.Contains(body, want) {
		t.Errorf("the page lacks\n%s\nin\n%s", want, body)
	}
	for _, lost := range []string{"About the warehouse", "the warehouse is open"} {
		if strings.Contains(body, lost) {
			t.Errorf("%q of the Background is on the page:\n%s", lost, body)
		}
	}
}

func TestARuleWithoutADescriptionIsItsTitleInEachBranchOfItsCases(t *testing.T) {
	body := pageOfCases(t, `Feature: Orders

  Rule: Stock is held

    Scenario: Placing a book
      When it is sent

    Scenario: Placing nothing
      When it is sent
`, []string{"Placing a book"}, []string{"Placing nothing"})

	for _, want := range []string{
		"#### Rule: Stock is held\n\n- **Placing a book**",
		"##### Rule: Stock is held\n\n- **Placing nothing**",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page lacks %q:\n%s", want, body)
		}
	}
}

func TestARulesDescriptionStandsOnceInAStepAndItsTitleInEachBranch(t *testing.T) {
	body := pageOfCases(t, `Feature: Orders

  Rule: Stock is held

    A held unit cannot be sold twice.

    Scenario: Placing a book
      When it is sent

    Scenario: Placing nothing
      When it is sent

    Scenario: Placing too much
      When it is sent

  Rule: Refusals are final

    A refused order stays refused.

    Scenario: Placing a ghost
      When it is sent
`, []string{"Placing a book"}, []string{"Placing nothing", "Placing a ghost"}, []string{"Placing too much"})

	for _, want := range []string{
		"#### Rule: Stock is held\n\nA held unit cannot be sold twice.\n\n- **Placing a book**",
		"##### Rule: Stock is held\n\n- **Placing nothing**",
		"##### Rule: Refusals are final\n\nA refused order stays refused.\n\n- **Placing a ghost**",
		"##### Rule: Stock is held\n\n- **Placing too much**",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page lacks %q:\n%s", want, body)
		}
	}
	if n := strings.Count(body, "A held unit cannot be sold twice."); n != 1 {
		t.Errorf("the description stands %d times:\n%s", n, body)
	}
}

func TestARulesDescriptionStandsOnThePageOfEachInputOfItsCases(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "stock", "tests", "flows.yaml"), stock)
	writeFile(t, filepath.Join(root, "shop", "tests", "features", "a.feature"), `Feature: Orders

  Rule: Stock is held

    A held unit cannot be sold twice.

    Scenario: Placing a book
      When it is sent

    Scenario: Returning a book
      When it is sent
`)
	sends := []string{"grpc /stock.S/Reserve"}
	writeFile(t, filepath.Join(root, "shop", "tests", "flows.yaml"), string(encode(Doc{Specrig: Format, Service: "shop", Inputs: []Input{
		{Message: "http POST /orders", Branches: []Branch{{Sends: sends, Cases: []string{"a.feature: Placing a book"}}}},
		{Message: "http POST /returns", Branches: []Branch{{Sends: sends, Cases: []string{"a.feature: Returning a book"}}}},
	}})))

	files, _, err := Assemble(root, filepath.Join(root, "docs", "flows.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, page := range []string{"shop-http-post-orders.md", "shop-http-post-returns.md"} {
		if body := string(files[filepath.Join(root, "docs", page)]); !strings.Contains(body, "A held unit cannot be sold twice.") {
			t.Errorf("%s lacks the description:\n%s", page, body)
		}
	}
}

func TestCasesOfARuleStandTogetherAndThoseOfNoneComeFirst(t *testing.T) {
	body := pageOfCases(t, `Feature: Orders

  # Nothing is held for nothing.
  Scenario: Placing nothing
    When it is sent

  Rule: Books

    Scenario: A placed book
      When it is sent

    Scenario: Z placed book
      When it is sent

  Rule: Pens

    Scenario: B placed pen
      When it is sent
`, []string{"A placed book", "B placed pen", "Placing nothing", "Z placed book"})

	want := "- **Placing nothing** · `a.feature`\n\n  Nothing is held for nothing.\n\n" +
		"#### Rule: Books\n\n- **A placed book** · `a.feature`\n\n- **Z placed book** · `a.feature`\n\n" +
		"#### Rule: Pens\n\n- **B placed pen** · `a.feature`\n\n"
	if !strings.Contains(body, want) {
		t.Errorf("the page lacks\n%s\nin\n%s", want, body)
	}
}

func TestAFileOfCommentsKeepsEveryLineOfItsPage(t *testing.T) {
	body := pageOfCases(t, `Feature: Orders

  # A book is held.
  # For a day.
  #
  # Then it is sold.
  Scenario: Placing a book
    When it is sent

  Scenario: Placing a pen
    When it is sent
`, []string{"Placing a book", "Placing a pen"})

	want := "- **Placing a book** · `a.feature`\n\n  A book is held. For a day.\n\n  Then it is sold.\n\n" +
		"- **Placing a pen** · `a.feature`\n\n"
	if !strings.Contains(body, want) {
		t.Errorf("the page lacks\n%s\nin\n%s", want, body)
	}
	if strings.Contains(body, "Rule:") {
		t.Errorf("a file without rules got one:\n%s", body)
	}
}

func TestADescriptionComesTogetherInParagraphsAndEndsAtTheFirstStep(t *testing.T) {
	body := pageOfCases(t, `Feature: Orders

  Scenario: Placing a book

    A book is held
    for a day.

    Then it is sold, and this is prose no longer.
    Given it is sent
`, []string{"Placing a book"})

	if want := "- **Placing a book** · `a.feature`\n\n  A book is held for a day.\n\n"; !strings.Contains(body, want) {
		t.Errorf("the page lacks %q:\n%s", want, body)
	}
	for _, lost := range []string{"prose no longer", "it is sent"} {
		if strings.Contains(body, lost) {
			t.Errorf("%q of a step is on the page:\n%s", lost, body)
		}
	}
}

func TestADescriptionTakesThePlaceOfTheCommentAboveItsCase(t *testing.T) {
	body := pageOfCases(t, `Feature: Orders

  # Above the book.
  Scenario: Placing a book
    A book is held.

    When it is sent

  # Above the pen.
  Scenario: Placing a pen
    When it is sent
`, []string{"Placing a book", "Placing a pen"})

	want := "- **Placing a book** · `a.feature`\n\n  A book is held.\n\n" +
		"- **Placing a pen** · `a.feature`\n\n  Above the pen.\n\n"
	if !strings.Contains(body, want) {
		t.Errorf("the page lacks\n%s\nin\n%s", want, body)
	}
	if strings.Contains(body, "Above the book") {
		t.Errorf("the comment stands beside a description:\n%s", body)
	}
}

func TestOutlineRowsInheritTheRuleAndTheDescription(t *testing.T) {
	body := pageOfCases(t, `Feature: Orders

  Rule: Stock is held

    Scenario Outline: Placing fails: <case>
      Nothing is held for a broken order.

      When <case> is sent

      Examples: The first
        About the rows, not the outline.

        | case   |
        | a book |

      Examples: The second
        | case  |
        | a pen |
`, []string{"Placing fails: a book", "Placing fails: a pen"})

	want := "#### Rule: Stock is held\n\n" +
		"- **Placing fails: a book** · `a.feature`\n\n  Nothing is held for a broken order.\n\n" +
		"- **Placing fails: a pen** · `a.feature`\n\n  Nothing is held for a broken order.\n\n"
	if !strings.Contains(body, want) {
		t.Errorf("the page lacks\n%s\nin\n%s", want, body)
	}
	if strings.Contains(body, "About the rows") {
		t.Errorf("the description of the Examples is on the page:\n%s", body)
	}
}

func TestACommentAmongStepsOrRowsIsNotOnThePage(t *testing.T) {
	body := pageOfCases(t, `Feature: Orders

  Scenario Outline: Placing <what>
    # Among the steps.
    Given it is ready
    # Among the steps too.
    When <what> is sent
    # After the steps.

    Examples:
      | what   |
      # Among the rows.
      | a book |
`, []string{"Placing a book"})

	if want := "- **Placing a book** · `a.feature`\n\n"; !strings.Contains(body, want) {
		t.Errorf("the page lacks %q:\n%s", want, body)
	}
	if strings.Contains(body, "Among") || strings.Contains(body, "After the steps") {
		t.Errorf("a comment among the steps is on the page:\n%s", body)
	}
}

func TestACommentInADescriptionIsNotPartOfIt(t *testing.T) {
	cases := parseFeature(`Feature: Orders

  Rule: Stock is held
    A book is held.
    # Not for the page.
    For a day.

    Scenario: Placing a book
      It is held.
      # Not for the page.
      For a day.
      When it is sent
`)

	if got := prose(cases[0].rule.text); got != "A book is held. For a day." {
		t.Errorf("the rule says %q", got)
	}
	if got := cases[0].reason(); got != "It is held. For a day." {
		t.Errorf("the case says %q", got)
	}
}

func TestKeywordsInATableOrADocstringAreNotKeywords(t *testing.T) {
	cases := parseFeature(`Feature: Orders

  Rule: Stock is held

    Background:
      Given the document
        """
        Scenario: In a docstring of the Background
        """

    Scenario: Placing a book
      Given the rows
        | Scenario: in a table |
        | Rule: in a table     |
        | # in a table         |
      When the document is
        """
        Rule: in a docstring
        Scenario: in a docstring
        Examples:
        | in a docstring |
        # in a docstring
        ` + "```" + `
        """
      And the other document is
        ` + "```" + `
        Scenario: in another docstring
        """
        ` + "```" + `

    # Above the pen.
    Scenario: Placing a pen
      When it is sent
`)

	var titles []string
	for _, c := range cases {
		titles = append(titles, c.title)
	}
	if want := []string{"Placing a book", "Placing a pen"}; !slices.Equal(titles, want) {
		t.Fatalf("cases %q, want %q", titles, want)
	}
	if cases[1].rule.title != "Stock is held" || cases[0].rule != cases[1].rule {
		t.Errorf("a docstring moved the rule: %q, %q", cases[0].rule.title, cases[1].rule.title)
	}
	if cases[0].reason() != "" || cases[1].reason() != "Above the pen." {
		t.Errorf("the cases say %q, %q", cases[0].reason(), cases[1].reason())
	}
}

func TestASeparatorBeforeTheFirstStepIsProseNotADocstring(t *testing.T) {
	cases := parseFeature(`Feature: Orders

  Background:
    """
    Given it is ready

  Scenario: Placing a book
    """
    Held for a day.
    When it is sent

  Scenario: Placing a pen
    When it is sent
`)

	if len(cases) != 2 {
		t.Fatalf("%d cases, want 2: %+v", len(cases), cases)
	}
	if got := cases[0].reason(); got != `""" Held for a day.` {
		t.Errorf("the case says %q", got)
	}
}

func TestAScenarioWithoutATitleEndsTheDescriptionBeforeIt(t *testing.T) {
	cases := parseFeature(`Feature: Orders

  Rule: Stock is held
    For a day.

    Scenario:
      When it is sent
`)

	if len(cases) != 1 || prose(cases[0].rule.text) != "For a day." {
		t.Errorf("%+v", cases)
	}
}

func TestExamplesBeforeAnyScenarioAreNoCase(t *testing.T) {
	if cases := parseFeature("Feature: Orders\n\n  Examples:\n    | what   |\n    | a book |\n"); len(cases) != 0 {
		t.Errorf("%+v", cases)
	}
}

func TestAnEscapedPipeIsPartOfAnOutlineRowsTitle(t *testing.T) {
	cases := parseFeature("Feature: Tax\n\n  Scenario Outline: Priced at <rate>\n\n" +
		"    Examples:\n      | rate        |\n      | 10 \\| 20 % |\n")

	if got := cases[len(cases)-1].title; got != "Priced at 10 | 20 %" {
		t.Errorf("title %q, want the row's value with its pipe", got)
	}
}

func TestARussianFileIsOnThePageLikeAnEnglishOne(t *testing.T) {
	body := pageOfCases(t, `# language: ru
Функция: Заказы

  Правило: Товар резервируется до заказа

    Зарезервированное нельзя продать дважды.
    И никто другой его не видит.

    Предыстория: Склад
      О складе, а не о правиле.

      Допустим склад открыт

    Сценарий: Заказ книги
      Книга резервируется на день.

      Когда заказ отправлен

    # Над ручкой.
    Сценарий: Заказ ручки
      Когда заказ отправлен

    Структура сценария: Отказ: <что>
      Для ошибочного заказа ничего не резервируется.

      Когда отправлено <что>

      Примеры: Первые
        О строках, а не о структуре.

        | что   |
        | книга |

      Примеры: Вторые
        | что   |
        | ручка |
`, []string{"Заказ книги", "Заказ ручки", "Отказ: книга", "Отказ: ручка"})

	want := "#### Rule: Товар резервируется до заказа\n\n" +
		"Зарезервированное нельзя продать дважды. И никто другой его не видит.\n\n" +
		"- **Заказ книги** · `a.feature`\n\n  Книга резервируется на день.\n\n" +
		"- **Заказ ручки** · `a.feature`\n\n  Над ручкой.\n\n" +
		"- **Отказ: книга** · `a.feature`\n\n  Для ошибочного заказа ничего не резервируется.\n\n" +
		"- **Отказ: ручка** · `a.feature`\n\n  Для ошибочного заказа ничего не резервируется.\n\n"
	if !strings.Contains(body, want) {
		t.Errorf("the page lacks\n%s\nin\n%s", want, body)
	}
	for _, lost := range []string{"О складе", "склад открыт", "О строках", "language"} {
		if strings.Contains(body, lost) {
			t.Errorf("%q is on the page:\n%s", lost, body)
		}
	}
}

func TestAStepKeywordWithoutASpaceEndsADescription(t *testing.T) {
	cases := parseFeature(`# language: ja
機能: 注文

  シナリオ: 本の注文
    本は一日確保される。

    前提本がある
`)

	if len(cases) != 1 || cases[0].reason() != "本は一日確保される。" {
		t.Errorf("%+v", cases)
	}
}

func TestKeywordsAreThoseOfTheLanguageAtTheTopOfTheFile(t *testing.T) {
	russian := "Функция: Заказы\n\n  Сценарий: Заказ книги\n"
	english := "Feature: Orders\n\n  Scenario: Placing a book\n"
	for _, c := range []struct {
		body string
		want []string
	}{
		{"# language: ru\n" + russian, []string{"Заказ книги"}},
		{"# About orders\n\n  #language:ru\n" + russian, []string{"Заказ книги"}},
		{"# language: ru\r\nФункция: Заказы\r\n\r\n  Сценарий: Заказ книги\r\n", []string{"Заказ книги"}},
		{"\xef\xbb\xbf# language: ru\n" + russian, []string{"Заказ книги"}},
		{"@orders\n# language: ru\n" + russian, nil},
		{"Функция: Заказы\n# language: ru\n\n  Сценарий: Заказ книги\n", nil},
		{russian, nil},
		{"# language: ru\n" + english, nil},
		{"# language: nowhere\n" + english, []string{"Placing a book"}},
	} {
		var titles []string
		for _, parsed := range parseFeature(c.body) {
			titles = append(titles, parsed.title)
		}
		if !slices.Equal(titles, c.want) {
			t.Errorf("cases %q, want %q from\n%s", titles, c.want, c.body)
		}
	}
}

// go test keeps a / of the title in the subtest's name, after the one that nests it.
func TestATitleWithASlashIsLabelledByItsFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "refunds.feature"), "Feature: Refunds\n\n  Scenario: A refund goes through POST /refunds\n    When it is sent\n")
	records := []trace.Record{{Seq: 1, Scenario: "TestFeatures/A_refund_goes_through_POST_/refunds", Dir: "in", Kind: "http", Name: "POST /refunds"}}

	doc := build("shop", records, readFeatures(dir))

	got := doc.Inputs[0].Branches[0].Cases
	if len(got) != 1 || got[0] != "refunds.feature: A refund goes through POST /refunds" {
		t.Errorf("cases %v, want the one of refunds.feature", got)
	}
}

// A new method and a saved one pay through one route; what tells their sends
// apart is the field each case carried, while a field every case carries tells nothing.
func TestAMessageNeedsTheFieldsThatTellItsSendersApart(t *testing.T) {
	got := func(seq int64, scenario, body, refusal string) trace.Record {
		return trace.Record{Seq: seq, Scenario: scenario, Dir: "in", Kind: "http", Name: "POST /payments", Request: json.RawMessage(body), Error: refusal}
	}
	charge := func(seq int64, scenario, method, body string) trace.Record {
		return trace.Record{Seq: seq, Scenario: scenario, Dir: "out", Kind: "grpc", Name: "/pay.Pay/" + method, Request: json.RawMessage(body)}
	}
	doc := build("gateway", []trace.Record{
		got(1, "TestFeatures/A_new_card", `{"gateway":"sber","invoices":[1]}`, ""),
		charge(2, "TestFeatures/A_new_card", "Charge", `{"gateway":"sber"}`),
		got(3, "TestFeatures/A_saved_card", `{"methodId":"m","invoices":[1],"comment":"x"}`, ""),
		charge(4, "TestFeatures/A_saved_card", "ChargeMethod", `{"methodId":"m"}`),
		got(5, "TestFeatures/Another_saved_card", `{"methodId":"n","invoices":[2]}`, ""),
		charge(6, "TestFeatures/Another_saved_card", "ChargeMethod", `{"methodId":"n"}`),
		got(7, "TestFeatures/Nothing_to_pay", `{}`, "HTTP 400"),
	}, features{})

	want := map[string][]string{
		"grpc /pay.Pay/Charge":       {"gateway"},
		"grpc /pay.Pay/ChargeMethod": {"methodId"},
	}
	if !reflect.DeepEqual(doc.Inputs[0].Needs, want) {
		t.Errorf("needs %v, want %v", doc.Inputs[0].Needs, want)
	}
	for _, b := range doc.Inputs[0].Branches {
		if slices.Contains(b.Sends, "grpc /pay.Pay/ChargeMethod") && !slices.Equal(b.Sent["grpc /pay.Pay/ChargeMethod"], []string{"methodId"}) {
			t.Errorf("sent %v, want the fields of the charge", b.Sent)
		}
	}
}

// Most cases name a unit, and the one redeeming a promo does too; the promo is
// what redeems it, so a contract for a unit type with a promo still does.
func TestAFieldCorrelatedInTheFixturesIsNoNeed(t *testing.T) {
	create := func(seq int64, scenario, body string, sends ...string) []trace.Record {
		out := []trace.Record{{Seq: seq, Scenario: scenario, Dir: "in", Kind: "grpc", Name: "/contract.Contracts/Create", Request: json.RawMessage(body)}}
		for i, m := range sends {
			out = append(out, trace.Record{Seq: seq + int64(i) + 1, Scenario: scenario, Dir: "out", Kind: "grpc", Name: m, Request: json.RawMessage(`{}`)})
		}
		return out
	}
	var records []trace.Record
	records = append(records, create(1, "TestFeatures/A_free_unit_is_picked", `{"unitType":"S"}`, "/warehouse.Units/Find")...)
	records = append(records, create(10, "TestFeatures/A_named_unit", `{"unitType":"S","unit":"A1"}`, "/warehouse.Units/Get")...)
	records = append(records, create(20, "TestFeatures/A_promo_is_redeemed", `{"unitType":"S","unit":"A1","promo":"P"}`, "/warehouse.Units/Get", "/price.Prices/UsePromo")...)
	doc := build("dungeon", records, features{})

	want := map[string][]string{
		"grpc /price.Prices/UsePromo": {"promo"},
		"grpc /warehouse.Units/Find":  nil,
		"grpc /warehouse.Units/Get":   {"unit"},
	}
	for m, fields := range want {
		if got := doc.Inputs[0].Needs[m]; !slices.Equal(got, fields) {
			t.Errorf("%s needs %v, want %v", m, got, fields)
		}
	}
}

func TestASendWhoseNeedsNothingCarriesIsFoldedToANote(t *testing.T) {
	gateway := in("gateway", "http POST /payments",
		Branch{Sends: []string{"grpc /auth.Auth/Summon", "grpc /pay.Pay/Charge"}, Cases: []string{"b"}},
		Branch{Sends: []string{"grpc /auth.Auth/Summon", "grpc /pay.Pay/ChargeMethod"}, Cases: []string{"c"}})
	gateway.input.Needs = map[string][]string{"grpc /pay.Pay/Charge": {"gateway"}, "grpc /pay.Pay/ChargeMethod": {"methodId"}}
	paying := func(entry string, fields ...string) project {
		return projectOf(
			in("portal", entry, Branch{
				Sends: []string{"http POST /payments"},
				Sent:  map[string][]string{"http POST /payments": fields},
				Cases: []string{"a"},
			}),
			gateway,
			in("auth", "grpc /auth.Auth/Summon", Branch{Cases: []string{"f"}}),
			in("pay", "grpc /pay.Pay/Charge", Branch{Cases: []string{"d"}}),
			in("pay", "grpc /pay.Pay/ChargeMethod", Branch{Cases: []string{"e"}}),
		)
	}

	p := paying("ui the visitor pays by card", "gateway", "invoices[]")
	order, _ := p.walk("portal: ui the visitor pays by card")
	if slices.Contains(order, "pay: grpc /pay.Pay/ChargeMethod") {
		t.Errorf("the flow follows a send nothing reaching it can make: %v", order)
	}
	body := string(page(p, Flow{Id: "portal-ui-the-visitor-pays-by-card", Entry: "portal: ui the visitor pays by card", Steps: order}, true))
	for _, want := range []string{
		"svc_gateway->>svc_pay: 4 · Pay.Charge",
		"Note over svc_gateway: not followed: Pay.ChargeMethod → pay — sent only in cases that carried methodId, which nothing reaching it carries",
		"- not followed here: Pay.ChargeMethod → pay — sent only in cases that carried methodId, which nothing reaching it carries",
		"- Auth.Summon → auth; Pay.ChargeMethod → pay — 1 case",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "opt ") {
		t.Errorf("the saved card's cases still count against the charge:\n%s", body)
	}

	saved := paying("ui the visitor pays by a saved card", "methodId")
	if order, _ := saved.walk("portal: ui the visitor pays by a saved card"); !slices.Contains(order, "pay: grpc /pay.Pay/ChargeMethod") || slices.Contains(order, "pay: grpc /pay.Pay/Charge") {
		t.Errorf("a saved card should reach the method charge alone: %v", order)
	}
}

func TestRefusalsOfDifferentKindsAreDifferentBranches(t *testing.T) {
	run := func(scenario, refusal string, seq int64) trace.Record {
		return trace.Record{Seq: seq, Scenario: scenario, Dir: "in", Kind: "grpc", Name: "/contract.Contracts/Close", Error: refusal}
	}
	doc := build("dungeon", []trace.Record{
		run("TestFeatures/Closing_an_unknown_contract", "NotFound contract_not_found", 1),
		run("TestFeatures/Closing_a_closed_contract", "FailedPrecondition contract_is_closed", 2),
	}, features{})

	if got := len(doc.Inputs[0].Branches); got != 2 {
		t.Errorf("%d branches, want one per refusal: %+v", got, doc.Inputs[0].Branches)
	}
}
