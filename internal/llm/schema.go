package llm

import "github.com/pdd-tech/pdd-assistant/internal/search"

const schemaVersion = "grounded-answer-schema-v2-profiles"

type jsonSchema struct {
	Name   string         `json:"name"`
	Strict bool           `json:"strict"`
	Schema map[string]any `json:"schema"`
}

func answerFormat(docs []search.Document, structured bool, requireQuotes bool) *responseFormat {
	if !structured {
		return &responseFormat{Type: "json_object"}
	}
	source := map[string]any{"type": "string"}
	if len(docs) > 0 {
		ids := make([]string, 0, len(docs))
		for _, doc := range docs {
			ids = append(ids, doc.ID)
		}
		source["enum"] = ids
	}
	part := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"text":   map[string]any{"type": "string"},
			"source": source,
			"quotes": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
		"required":             []string{"text", "source", "quotes"},
		"additionalProperties": false,
	}
	if !requireQuotes {
		delete(part["properties"].(map[string]any), "quotes")
		part["required"] = []string{"text", "source"}
	}
	return &responseFormat{Type: "json_schema", JSONSchema: &jsonSchema{
		Name: "grounded_answer", Strict: true,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"refused": map[string]any{"type": "boolean"},
				"parts":   map[string]any{"type": "array", "items": part},
				"missing": map[string]any{"type": "string"},
			},
			"required":             []string{"refused", "parts", "missing"},
			"additionalProperties": false,
		},
	}}
}
