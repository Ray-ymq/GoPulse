package notification

import (
	"context"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
)

// Processor applies a decoded business event to durable notification state.
type Processor struct {
	repository *Repository
}

func NewProcessor(repository *Repository) (*Processor, error) {
	if repository == nil {
		return nil, ErrInvalidArgument
	}
	return &Processor{repository: repository}, nil
}

func (processor *Processor) Process(ctx context.Context, envelope bus.Envelope) error {
	_, err := processor.repository.Insert(ctx, envelope)
	return err
}
