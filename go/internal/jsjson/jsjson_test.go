package jsjson

import "testing"

func TestIndentMatchesJSONStringify(t *testing.T) {
	cases := map[string]string{
		`{"duration":624.0,"x":1e21,"y":0.0000001,"z":-0.0}`: "{\n  \"duration\": 624,\n  \"x\": 1e+21,\n  \"y\": 1e-7,\n  \"z\": 0\n}",
		`{"b":1,"a":{"c":[]},"d":{}}`:                        "{\n  \"b\": 1,\n  \"a\": {\n    \"c\": []\n  },\n  \"d\": {}\n}",
		`["<a&b>","\u2028","é","\u0001","q\"\\"]`:            "[\n  \"<a&b>\",\n  \"\u2028\",\n  \"é\",\n  \"\\u0001\",\n  \"q\\\"\\\\\"\n]",
		`{"k":1,"k":2,"j":3}`:                                "{\n  \"k\": 2,\n  \"j\": 3\n}",
		`"str"`:                                              `"str"`,
		`null`:                                               `null`,
		`12345678901234567890`:                               `12345678901234567000`,
	}
	for in, want := range cases {
		got, err := Indent([]byte(in))
		if err != nil || got != want {
			t.Errorf("%s\n got %q (%v)\nwant %q", in, got, err, want)
		}
	}
	if _, err := Indent([]byte(`{} x`)); err == nil {
		t.Error("trailing data accepted")
	}
}
