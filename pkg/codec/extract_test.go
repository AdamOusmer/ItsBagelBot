package codec

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

const doc = `{
	"account_id":"abc-123",
	"level":74,
	"ratio":1.75,
	"active":true,
	"missing_child":null,
	"name":"say \"hi\"",
	"profile":{"region":"eu","wins":12},
	"modes":["solo","duo","squad"],
	"stats":{"br_kills_solo":41,"br_wins_solo":3,"br_kills_duo":58,"other":9}
}`

func TestExtractScalars(t *testing.T) {
	if got, err := ExtractString([]byte(doc), "account_id"); err != nil || got != "abc-123" {
		t.Fatalf("ExtractString() = %q, %v", got, err)
	}
	if got, err := ExtractInt([]byte(doc), "level"); err != nil || got != 74 {
		t.Fatalf("ExtractInt() = %d, %v", got, err)
	}
	if got, err := ExtractFloat([]byte(doc), "ratio"); err != nil || got != 1.75 {
		t.Fatalf("ExtractFloat() = %v, %v", got, err)
	}
	if got, err := ExtractBool([]byte(doc), "active"); err != nil || !got {
		t.Fatalf("ExtractBool() = %v, %v", got, err)
	}
	// Escapes are resolved rather than returned raw.
	if got, err := ExtractString([]byte(doc), "name"); err != nil || got != `say "hi"` {
		t.Fatalf("ExtractString(name) = %q, %v", got, err)
	}
}

func TestExtractNestedPath(t *testing.T) {
	if got, err := ExtractString([]byte(doc), "profile", "region"); err != nil || got != "eu" {
		t.Fatalf("nested ExtractString() = %q, %v", got, err)
	}
	if got, err := ExtractInt([]byte(doc), "profile", "wins"); err != nil || got != 12 {
		t.Fatalf("nested ExtractInt() = %d, %v", got, err)
	}
}

func TestExtractMissingPathIsErrNotFound(t *testing.T) {
	_, err := ExtractString([]byte(doc), "profile", "nope")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	// The path is named in the message so a failure is diagnosable.
	if !strings.Contains(err.Error(), "nope") {
		t.Fatalf("error omits the path: %v", err)
	}
}

// A present-but-null field is not a missing field.
func TestExtractNullIsNotMissing(t *testing.T) {
	_, kind, err := ExtractValue([]byte(doc), "missing_child")
	if err != nil {
		t.Fatalf("ExtractValue(null) errored: %v", err)
	}
	if kind != KindNull {
		t.Fatalf("kind = %v, want null", kind)
	}
}

func TestExtractValueKinds(t *testing.T) {
	for _, tc := range []struct {
		path string
		want Kind
	}{
		{"account_id", KindString},
		{"level", KindNumber},
		{"active", KindBool},
		{"profile", KindObject},
		{"modes", KindArray},
		{"missing_child", KindNull},
	} {
		_, kind, err := ExtractValue([]byte(doc), tc.path)
		if err != nil {
			t.Fatalf("ExtractValue(%s): %v", tc.path, err)
		}
		if kind != tc.want {
			t.Fatalf("ExtractValue(%s) kind = %v, want %v", tc.path, kind, tc.want)
		}
	}
	// An object value comes back as valid JSON and round-trips through Unmarshal.
	raw, _, err := ExtractValue([]byte(doc), "profile")
	if err != nil {
		t.Fatal(err)
	}
	var profile struct {
		Region string `json:"region"`
		Wins   int    `json:"wins"`
	}
	if err := Unmarshal(raw, &profile); err != nil {
		t.Fatalf("object value is not valid JSON: %v", err)
	}
	if profile.Region != "eu" || profile.Wins != 12 {
		t.Fatalf("round-trip = %+v", profile)
	}
}

// The wide-object case: aggregate the keys matching a prefix, ignore the rest.
func TestExtractEachAggregates(t *testing.T) {
	var total int64
	var seen int
	err := ExtractEach([]byte(doc), func(key, value []byte, kind Kind) error {
		seen++
		if kind != KindNumber || !strings.HasPrefix(string(key), "br_kills_") {
			return nil
		}
		n, err := ParseInt(value)
		if err != nil {
			return err
		}
		total += n
		return nil
	}, "stats")
	if err != nil {
		t.Fatalf("ExtractEach: %v", err)
	}
	if seen != 4 {
		t.Fatalf("visited %d members, want 4", seen)
	}
	if total != 99 { // 41 + 58
		t.Fatalf("total = %d, want 99", total)
	}
}

// An error raised by the callback is returned unchanged, so a caller's own
// sentinel survives and can be used to stop the walk early.
func TestExtractEachPropagatesCallbackError(t *testing.T) {
	stop := errors.New("stop")
	var visited int
	err := ExtractEach([]byte(doc), func(_, _ []byte, _ Kind) error {
		visited++
		return stop
	}, "stats")
	if !errors.Is(err, stop) {
		t.Fatalf("want the callback's own error, got %v", err)
	}
	if visited != 1 {
		t.Fatalf("walk continued past the error: visited %d", visited)
	}
}

func TestExtractArray(t *testing.T) {
	var got []string
	err := ExtractArray([]byte(doc), func(value []byte, kind Kind) error {
		if kind != KindString {
			t.Fatalf("element kind = %v", kind)
		}
		s, err := ParseString(value)
		if err != nil {
			return err
		}
		got = append(got, s)
		return nil
	}, "modes")
	if err != nil {
		t.Fatalf("ExtractArray: %v", err)
	}
	if strings.Join(got, ",") != "solo,duo,squad" {
		t.Fatalf("elements = %v", got)
	}
}

func TestExtractEachMissingPath(t *testing.T) {
	err := ExtractEach([]byte(doc), func(_, _ []byte, _ Kind) error { return nil }, "nope")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestExtractMalformedDocument(t *testing.T) {
	if _, err := ExtractString([]byte(`{"a":`), "a"); err == nil {
		t.Fatal("malformed document accepted")
	}
}

// The reason this API exists is that its cost does not grow with the document,
// so that is what is asserted: a wide object must cost no more allocations than
// a narrow one. The constant is the adapter callback ExtractEach hands to the
// parser; nothing is allocated per member.
func TestExtractEachAllocationIsFlat(t *testing.T) {
	scan := func(data []byte) float64 {
		return testing.AllocsPerRun(50, func() {
			var total int64
			_ = ExtractEach(data, func(key, value []byte, kind Kind) error {
				if kind == KindNumber && strings.HasPrefix(string(key), "br_kills_") {
					n, _ := ParseInt(value)
					total += n
				}
				return nil
			}, "stats")
			sinkInt = total
		})
	}

	narrow := scan(statsDoc(5))
	wide := scan(statsDoc(5000))
	if narrow != wide {
		t.Fatalf("allocations scale with document size: %v members-5 vs %v members-5000", narrow, wide)
	}
	if wide > 1 {
		t.Fatalf("ExtractEach allocated %v times per call, want at most 1", wide)
	}
}

// statsDoc builds a flat stats object with n counters, the shape of an upstream
// stats payload.
func statsDoc(n int) []byte {
	var b strings.Builder
	b.WriteString(`{"stats":{`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`"br_kills_m`)
		b.WriteString(strconv.Itoa(i))
		b.WriteString(`":`)
		b.WriteString(strconv.Itoa(i))
	}
	b.WriteString(`}}`)
	return []byte(b.String())
}

var sinkInt int64
