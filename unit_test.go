package fkqdrant

import (
	"regexp"
	"testing"
	"time"

	"github.com/qdrant/go-client/qdrant"
)

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-([0-9a-f])[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNewID(t *testing.T) {
	a, b := NewID(), NewID()
	if m := uuidRe.FindStringSubmatch(a); m == nil || m[1] != "4" {
		t.Fatalf("NewID() = %q, want a v4 UUID", a)
	}
	if a == b {
		t.Fatal("NewID returned the same value twice")
	}
}

func TestIDFrom(t *testing.T) {
	// Python: uuid.uuid5(uuid.UUID(IDNamespace), "65f1a2b3c4d5e6f708091a2b")
	const want = "ea07cede-15c4-5f6a-b925-eced2fba728b"
	if got := IDFrom("65f1a2b3c4d5e6f708091a2b"); got != want {
		t.Fatalf("IDFrom = %q, want %q", got, want)
	}
	if IDFrom("a") == IDFrom("b") {
		t.Fatal("IDFrom collided")
	}
}

func TestPointID(t *testing.T) {
	if id := pointID("42"); id.GetNum() != 42 || idString(id) != "42" {
		t.Fatalf("pointID(42) = %v", id)
	}
	u := NewID()
	if id := pointID(u); id.GetUuid() != u || idString(id) != u {
		t.Fatalf("pointID(%s) = %v", u, id)
	}
}

type meta struct {
	Lang string   `json:"lang"`
	Tags []string `json:"tags"`
}

type doc struct {
	Text    string    `json:"text"`
	N       int64     `json:"n"`
	Big     int64     `json:"big"`
	Score   float64   `json:"score"`
	OK      bool      `json:"ok"`
	Meta    meta      `json:"meta"`
	At      time.Time `json:"at"`
	Skipped string    `json:"-"`
	Opt     *string   `json:"opt,omitempty"`
}

func TestPayloadRoundTrip(t *testing.T) {
	in := doc{
		Text: "привет", N: 7, Big: 1<<62 + 1, Score: 0.25, OK: true,
		Meta: meta{Lang: "ru", Tags: []string{"a", "b"}},
		At:   time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), Skipped: "x",
	}
	p, err := toPayload(in)
	if err != nil {
		t.Fatal(err)
	}
	if p["n"].GetIntegerValue() != 7 || p["score"].GetDoubleValue() != 0.25 {
		t.Fatalf("numbers stored wrong: n=%v score=%v", p["n"], p["score"])
	}
	if _, ok := p["opt"]; ok {
		t.Fatal("omitempty field was stored")
	}
	out, err := fromPayload[doc](p)
	if err != nil {
		t.Fatal(err)
	}
	in.Skipped = ""
	if out.Text != in.Text || out.N != in.N || out.Big != in.Big || out.Score != in.Score || !out.OK ||
		out.Meta.Lang != "ru" || len(out.Meta.Tags) != 2 || !out.At.Equal(in.At) {
		t.Fatalf("round trip:\n got %+v\nwant %+v", out, in)
	}
}

func TestPayloadMap(t *testing.T) {
	p, err := toPayload(map[string]any{"n": 3, "s": "x"})
	if err != nil {
		t.Fatal(err)
	}
	m, err := fromPayload[map[string]any](p)
	if err != nil {
		t.Fatal(err)
	}
	if m["n"] != int64(3) || m["s"] != "x" {
		t.Fatalf("got %#v", m)
	}
}

func TestPayloadNotObject(t *testing.T) {
	if _, err := toPayload("text"); err == nil {
		t.Fatal("want error for non-object payload")
	}
}

type botID string

func TestFilters(t *testing.T) {
	if got := Eq("bot_id", botID("b1")).GetField().GetMatch().GetKeyword(); got != "b1" {
		t.Fatalf("Eq string = %q", got)
	}
	if got := Eq("n", uint8(5)).GetField().GetMatch().GetInteger(); got != 5 {
		t.Fatalf("Eq int = %d", got)
	}
	if !Eq("ok", true).GetField().GetMatch().GetBoolean() {
		t.Fatal("Eq bool")
	}
	if got := In("lang", "ru", "en").GetField().GetMatch().GetKeywords().GetStrings(); len(got) != 2 {
		t.Fatalf("In strings = %v", got)
	}
	if got := In("n", 1, 2, 3).GetField().GetMatch().GetIntegers().GetIntegers(); len(got) != 3 {
		t.Fatalf("In ints = %v", got)
	}
	f := Must(Eq("a", "x"), qdrant.NewFilterAsCondition(MustNot(Eq("b", 1))))
	if len(f.GetMust()) != 2 || len(f.GetMust()[1].GetFilter().GetMustNot()) != 1 {
		t.Fatalf("Must = %v", f)
	}
}
