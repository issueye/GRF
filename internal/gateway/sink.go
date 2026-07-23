package gateway

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/grok-free-register/grok-reg/internal/cpa"
)

type Sink struct {
	store *Store
}

func OpenSink(ctx context.Context, databasePath, keyPath string) (*Sink, error) {
	store, err := OpenStore(ctx, databasePath, keyPath)
	if err != nil {
		return nil, err
	}
	return &Sink{store: store}, nil
}

func (s *Sink) Close() error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.Close()
}

func (s *Sink) Import(ctx context.Context, document cpa.Document) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("gateway sink is not initialized")
	}
	_, err := s.store.ImportAccountDocument(ctx, mustMarshal(document))
	return err
}

func mustMarshal(value any) []byte {
	data, _ := json.Marshal(value)
	return data
}
