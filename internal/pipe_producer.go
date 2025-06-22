package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

type PipeProducer struct {
	itemGetter        ItemGetter
	heartbeatInterval time.Duration
}

func NewPipeProducer(itemGetter ItemGetter, heartbeatInterval time.Duration) *PipeProducer {
	return &PipeProducer{
		itemGetter:        itemGetter,
		heartbeatInterval: heartbeatInterval,
	}
}

func (s *PipeProducer) Run(ctx context.Context, out io.WriteCloser, in io.Reader) error {
	enc := json.NewEncoder(out)
	dec := json.NewDecoder(in)

	var secRes *SecretResponse
	for {
		// log.Print("PipeProducer encoding")
		hReq := HeartbeatRequest{
			SecretResponse: secRes,
		}
		if err := enc.Encode(hReq); err != nil {
			return fmt.Errorf("failed to write heartbeat request: %s", err)
		}
		// log.Printf("PipeProducer encoded, secRes=%#v", secRes)

		var hRes HeartbeatResponse
		if err := dec.Decode(&hRes); err != nil {
			return fmt.Errorf("failed to read heartbeat response: %s", err)
		}
		// log.Printf("PipeProducer decoded, secReq=%#v", hRes.SecretRequest)

		var err error
		secRes, err = s.handleSecretRequest(ctx, hRes.SecretRequest)
		if err != nil {
			return fmt.Errorf("failed to handle secret request: %s", err)
		}
		if secRes != nil {
			continue
		}

		// log.Print("PipeProducer will sleep or exit")
		select {
		case <-ctx.Done():
			return out.Close()
		case <-time.After(s.heartbeatInterval):
		}
	}
}

func (s *PipeProducer) handleSecretRequest(ctx context.Context, req *SecretRequest) (*SecretResponse, error) {
	if req == nil {
		return nil, nil
	}

	res := &SecretResponse{
		ID:    req.ID,
		Items: make([]SecretResponseItem, len(req.Items)),
	}
	for i, reqItem := range req.Items {
		var err error
		res.Items[i], err = s.handleSecretRequestItem(ctx, reqItem)
		if err != nil {
			return nil, err
		}
	}
	return res, nil
}

func (s *PipeProducer) handleSecretRequestItem(ctx context.Context, reqItem SecretRequestItem) (SecretResponseItem, error) {
	name := reqItem.ItemName
	input, err := s.itemGetter.GetItem(ctx, name)
	if err != nil {
		return SecretResponseItem{},
			fmt.Errorf("failed to get item: name=%s, err=%s", name, err)
	}

	result, err := runQuery(reqItem.Query, input)
	if err != nil {
		return SecretResponseItem{},
			fmt.Errorf("failed to run query: err=%s", err)
	}

	return SecretResponseItem{
		ItemName: name,
		Result:   result,
	}, nil
}
