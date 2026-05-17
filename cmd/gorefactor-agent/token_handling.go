package main

func (p *openAIProvider) Tokens() (int, int) { return p.promptToks, p.completionToks }
