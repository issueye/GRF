package gateway

import "time"

type Model struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

func buildModels() []Model {
	return []Model{
		{ID: "grok-4.5", Object: "model", Created: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Unix(), OwnedBy: "xai"},
	}
}

func ListModels() []Model {
	models := buildModels()
	return append([]Model(nil), models...)
}
