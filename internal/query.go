package internal

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/itchyny/gojq"
)

func runQuery(query, input string) (string, error) {
	q, err := gojq.Parse(query)
	if err != nil {
		return "", fmt.Errorf("failed to parse query: %s", query)
	}

	var res strings.Builder
	enc := json.NewEncoder(&res)
	dec := json.NewDecoder(strings.NewReader(input))
	for {
		var obj any
		if err := dec.Decode(&obj); err == io.EOF {
			break
		} else if err != nil {
			return "", fmt.Errorf("failed to parse input: %s", input)
		}

		iter := q.Run(obj)
		for {
			v, ok := iter.Next()
			if !ok {
				break
			}
			if err, ok := v.(error); ok {
				if err, ok := err.(*gojq.HaltError); ok && err.Value() == nil {
					break
				}
				return "", fmt.Errorf("failed to process query: %s", err)
			}

			if err := enc.Encode(v); err != nil {
				return "", fmt.Errorf("failed to marshal query result: %s", err)
			}
		}
	}
	return res.String(), nil
}
