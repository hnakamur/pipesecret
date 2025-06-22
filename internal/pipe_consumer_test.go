package internal

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

func TestPipeConsumer(t *testing.T) {
	var errProducer, errConsumer, errClient error
	ctx, cancel := context.WithCancel(t.Context())
	r, w := io.Pipe()

	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		itemGetter := newMockItemGetter(exampleItem)
		heartbeatInterval := time.Duration(time.Second)
		pipeProducer := NewPipeProducer(itemGetter, heartbeatInterval)
		if err := pipeProducer.Run(ctx, w, r); err != nil {
			errProducer = err
		}
	}()

	requestQueue := make(chan *PipeConsumerSecretRequest)
	go func() {
		defer wg.Done()
		pipeConsumer := NewPipeConsumer(requestQueue)
		if err := pipeConsumer.Run(ctx, r, w); err != nil {
			errConsumer = err
		}
	}()

	go func() {
		defer wg.Done()
		pipeConsumerClient := NewPipeConsumerClient(requestQueue)
		req := &SecretRequest{
			ID: "1",
			Items: []SecretRequestItem{
				{
					ItemName: "someName",
					Query:    `{"username": .fields[] | select(.id == "username").value, "password": .fields[] | select(.id == "password").value}`,
				},
			},
		}
		resp, err := pipeConsumerClient.RequestSecret(ctx, req)
		if err != nil {
			errClient = err
		}

		if got, want := len(resp.Items), 1; got != want {
			t.Errorf("item count mismatch, got=%d, want=%d", got, want)
		}
		if got, want := resp.Items[0].Result, canonicalizeJSON(t, `{"username":"username1","password":"my_password1"}`); got != want {
			t.Errorf("item mismatch, got=%s, want=%s", got, want)
		}
		cancel()
	}()
	wg.Wait()
	if err := errors.Join(errConsumer, errProducer, errClient); err != nil {
		t.Fatal(err)
	}
}

type mockItemGetter struct {
	mockResponse string
}

func newMockItemGetter(mockResponse string) *mockItemGetter {
	return &mockItemGetter{
		mockResponse: mockResponse,
	}
}

func (g *mockItemGetter) GetItem(_ context.Context, _ string) (string, error) {
	return g.mockResponse, nil
}
