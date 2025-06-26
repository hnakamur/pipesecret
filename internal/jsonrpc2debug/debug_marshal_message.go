package jsonrpc2debug

import (
	"encoding/json"
	"log/slog"

	"golang.org/x/exp/jsonrpc2"
)

type DebugMarshalMessage struct {
	Msg jsonrpc2.Message
}

func (m DebugMarshalMessage) LogValue() slog.Value {
	jsonBytes, err := jsonrpc2.EncodeMessage(m.Msg)
	if err != nil {
		panic(err)
	}

	var obj any
	if err := json.Unmarshal(jsonBytes, &obj); err != nil {
		panic(err)
	}

	return slog.AnyValue(obj)
}
