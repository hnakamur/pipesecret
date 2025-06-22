package internal

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

const exampleItem = `{
  "id": "id1",
  "title": "test1",
  "version": 1,
  "vault": {
    "id": "vault_id1",
    "name": "Private"
  },
  "category": "LOGIN",
  "last_edited_by": "foo",
  "created_at": "2025-06-20T10:46:56Z",
  "updated_at": "2025-06-20T10:46:56Z",
  "additional_information": "user1",
  "fields": [
    {
      "id": "username",
      "type": "STRING",
      "purpose": "USERNAME",
      "label": "username",
      "value": "username1",
      "reference": "op://Private/test1/username"
    },
    {
      "id": "password",
      "type": "CONCEALED",
      "purpose": "PASSWORD",
      "label": "password",
      "value": "my_password1",
      "entropy": 118.28349304199219,
      "reference": "op://Private/test1/password",
      "password_details": {
        "entropy": 118,
        "generated": true,
        "strength": "FANTASTIC",
        "history": ["history1"]
      }
    },
    {
      "id": "notesPlain",
      "type": "STRING",
      "purpose": "NOTES",
      "label": "notesPlain",
      "reference": "op://Private/test1/notesPlain"
    }
  ]
}`

func TestRunQuery(t *testing.T) {
	testCases := []struct {
		query string
		input string
		want  string
	}{
		{
			query: ".foo",
			input: `{"foo": 128}`,
			want:  `128` + "\n",
		},
		{
			query: ".a.b",
			input: `{"a": {"b": 42}}`,
			want:  `42` + "\n",
		},
		{
			query: `{(.id): .["10"].b}`,
			input: `{"id": "sample", "10": {"b": 42}}`,
			want:  `{"sample":42}` + "\n",
		},
		{
			query: `.[] | .id`,
			input: `[{"id":1},{"id":2},{"id":3}]`,
			want:  "1\n2\n3\n",
		},
		{
			query: `.a += 1 | .b *= 2`,
			input: `{"a":1,"b":2}`,
			want:  canonicalizeJSON(t, `{"a":2,"b":4}`),
		},
		{
			query: `. as {$a} ?// [$a] ?// $a | $a`,
			input: `{"a":1} [2] 3`,
			want:  "1\n2\n3\n",
		},
		{
			query: `{"username": .fields[] | select(.id == "username").value, "password": .fields[] | select(.id == "password").value}`,
			input: exampleItem,
			want:  canonicalizeJSON(t, `{"username":"username1","password":"my_password1"}`),
		},
	}
	for _, tc := range testCases {
		got, err := runQuery(tc.query, tc.input)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Errorf("result mismatch, query=%s, input=%s, got=%s, want=%s",
				tc.query, tc.input, got, tc.want)
		}
	}
}

func canonicalizeJSON(t *testing.T, input string) string {
	var res strings.Builder
	enc := json.NewEncoder(&res)
	dec := json.NewDecoder(strings.NewReader(input))
	for {
		var obj any
		if err := dec.Decode(&obj); err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("failed to parse input: %s", input)
		}

		if err := enc.Encode(obj); err != nil {
			t.Fatalf("failed to marshal query result: %s", err)
		}
	}
	return res.String()
}
