package main

import "encoding/json"

func (p *openAIProvider) addUsage(body []byte) {
	var u usageEnvelope
	if json.Unmarshal(body, &u) == nil {
		p.promptToks += u.Usage.PromptTokens
		p.completionToks += u.Usage.CompletionTokens
	}
}
