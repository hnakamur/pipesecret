package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
)

type PipeConsumer struct {
	requestQueue <-chan *PipeConsumerSecretRequest
}

type PipeConsumerSecretRequest struct {
	request    *SecretRequest
	responseCh chan<- *SecretResponse
}

func NewPipeConsumer(requestQueue <-chan *PipeConsumerSecretRequest) *PipeConsumer {
	return &PipeConsumer{
		requestQueue: requestQueue,
	}
}

func (s *PipeConsumer) Run(ctx context.Context, in io.Reader, out io.Writer) error {
	enc := json.NewEncoder(out)
	dec := json.NewDecoder(in)
	var responseCh chan<- *SecretResponse
	for {
		// log.Print("PipeConsumer decoding")
		var hReq HeartbeatRequest
		if err := dec.Decode(&hReq); err != nil {
			return fmt.Errorf("failed to read heartbeat request: %s", err)
		}
		// log.Printf("PipeConsumer decoded, secRes=%#v", hReq.SecretResponse)
		if responseCh != nil && hReq.SecretResponse != nil {
			responseCh <- hReq.SecretResponse
			responseCh = nil
		}

		var secReq *SecretRequest
		select {
		case svrSecReq := <-s.requestQueue:
			secReq = svrSecReq.request
			responseCh = svrSecReq.responseCh
		default:
		}

		hRes := HeartbeatResponse{
			SecretRequest: secReq,
		}
		if err := enc.Encode(hRes); err != nil {
			return fmt.Errorf("failed to write heartbeat response: %s", err)
		}
		// log.Printf("PipeConsumer encoded, secReq=%#v", secReq)

		select {
		case <-ctx.Done():
			return nil
		default:
		}
	}
}

type PipeConsumerClient struct {
	requestQueue chan<- *PipeConsumerSecretRequest
}

func NewPipeConsumerClient(requestQueue chan<- *PipeConsumerSecretRequest) *PipeConsumerClient {
	return &PipeConsumerClient{
		requestQueue: requestQueue,
	}
}

func (c *PipeConsumerClient) RequestSecret(ctx context.Context, req *SecretRequest) (*SecretResponse, error) {
	responseCh := make(chan *SecretResponse)
	c.requestQueue <- &PipeConsumerSecretRequest{
		request:    req,
		responseCh: responseCh,
	}

	select {
	case resp := <-responseCh:
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
