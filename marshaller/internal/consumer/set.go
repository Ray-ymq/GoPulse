package consumer

import (
	"context"
	"errors"
	"fmt"
)

type Stream struct {
	Name      string
	Kafka     *Kafka
	Processor *Processor
}

type Set struct {
	streams []Stream
}

func NewSet(streams ...Stream) (*Set, error) {
	if len(streams) == 0 {
		return nil, errors.New("at least one Kafka stream is required")
	}
	for _, stream := range streams {
		if stream.Kafka == nil || stream.Processor == nil {
			return nil, errors.New("Kafka stream is incomplete")
		}
	}
	return &Set{streams: append([]Stream(nil), streams...)}, nil
}

func (s *Set) Ready(ctx context.Context) error {
	for _, stream := range s.streams {
		if err := stream.Kafka.Ready(ctx); err != nil {
			return fmt.Errorf("%s: %w", stream.Name, err)
		}
	}
	return nil
}

func (s *Set) Run(ctx context.Context, logf func(string, ...any)) error {
	if s == nil || len(s.streams) == 0 {
		return errors.New("Kafka stream set is empty")
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan struct {
		name string
		err  error
	}, len(s.streams))
	for _, stream := range s.streams {
		stream := stream
		go func() {
			results <- struct {
				name string
				err  error
			}{stream.Name, stream.Kafka.Run(runCtx, stream.Processor, logf)}
		}()
	}
	var first error
	for range s.streams {
		result := <-results
		if result.err != nil && first == nil {
			first = fmt.Errorf("%s: %w", result.name, result.err)
			cancel()
		}
	}
	return first
}

func (s *Set) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}
	var first error
	for _, stream := range s.streams {
		if err := stream.Kafka.Close(ctx); err != nil && first == nil {
			first = fmt.Errorf("%s: %w", stream.Name, err)
		}
	}
	return first
}
