# Declarative Skill: Text Summarizer

## Purpose
Demonstrate a pure prompt-based capability with no code execution.

## Type
**Declarative** — LLM-only, no Wasm, no external access.

## Manifest

```yaml
id: core/summarize
name: Text Summarizer
type: declarative
version: 1.0.0
description: Summarizes text into concise bullet points

prompt: |
  You are a precise summarization assistant.
  
  Given the following text, produce exactly 3 bullet points that capture
  the key information. Be concise, factual, and objective.
  
  Do not add opinions or information not present in the source.
  
  Text to summarize:
  {{.Input}}

input_schema:
  type: object
  properties:
    text:
      type: string
      description: The text to summarize
  required: [text]

output_schema:
  type: object
  properties:
    summary:
      type: array
      items:
        type: string
```

## Execution Flow

```
User Input → Prompt Template → LLM Generate → Structured Output
```

## Security Properties

| Property | Value |
|----------|-------|
| External access | None |
| Side effects | None |
| Determinism | Non-deterministic (LLM) |
| Audit trail | Prompt + response logged |

## Test (Conceptual)

Since this is a declarative skill executed entirely by the LLM,
testing is performed by the agent selecting this skill and
invoking ModelProvider.Generate() with the templated prompt.

```go
// Agent pseudo-code
func handleSummarize(input string) string {
    skill := registry.Get("core/summarize")
    prompt := skill.TemplatePrompt(input)
    response := modelProvider.Generate(prompt)
    return response
}
```
