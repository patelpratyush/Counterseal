package strictjson

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecode(t *testing.T) {
	for _, raw := range []string{`{"id":1,"ID":2}`, `{"x":{"a":1,"a":2}}`, `{} {}`, `[1,`, strings.Repeat("[", 66) + "0" + strings.Repeat("]", 66)} {
		var v any
		if Decode([]byte(raw), &v) == nil {
			t.Errorf("accepted ambiguous input")
		}
	}
	var v map[string]any
	if err := Decode([]byte(`{"n":9007199254740993}`), &v); err != nil {
		t.Fatal(err)
	}
	if v["n"] != json.Number("9007199254740993") {
		t.Fatal("numeric precision lost")
	}
	var s struct {
		ID string `json:"id"`
	}
	if err := Decode([]byte(`{"extra":true}`), &s); err == nil {
		t.Fatal("unknown field accepted")
	}
}
