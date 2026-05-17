package main

import "context"

// CompleteSchema sends response_format=json_schema. Ollama (>=0.5) and
// OpenAI structured outputs both honor this on the /chat/completions
// endpoint; the model is grammar-constrained to the schema.
func (p *openAIProvider) CompleteSchema(ctx context.Context, system, user, schema string) (string, error) {
	return p.complete(ctx, system, user, schema)
}
