package pipereverserpc

import (
	"context"
	"errors"
	"io"
	"log"
	"sync"
	"testing"
	"time"

	"golang.org/x/exp/jsonrpc2"
)

func TestConsumer(t *testing.T) {
	var errProducer, errConsumer error
	ctx, cancel := context.WithCancel(t.Context())
	r, w := io.Pipe()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		heartbeatInterval := 100 * time.Millisecond
		producer := NewProducer(jsonrpc2.HeaderFramer(), heartbeatInterval)
		if err := producer.Run(ctx, w, r); err != nil {
			errProducer = err
		}
	}()
	go func() {
		defer wg.Done()
		consumer := NewConsumer(jsonrpc2.HeaderFramer())
		if err := consumer.Run(ctx, r, w); err != nil {
			errConsumer = err
		}
	}()

	time.Sleep(500 * time.Millisecond)
	log.Printf("calling cancel")
	cancel()
	log.Printf("called cancel")
	wg.Wait()
	if errProducer != nil && !errors.Is(errProducer, context.Canceled) {
		t.Errorf("errProducer=%v", errProducer)
	}
	if errConsumer != nil && !errors.Is(errConsumer, io.EOF) && !errors.Is(errConsumer, context.Canceled) {
		t.Errorf("errConsumer=%v", errConsumer)
	}
}
