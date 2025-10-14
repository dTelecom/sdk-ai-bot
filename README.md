## Voice AI Bot SDK for dTelecom (LiveKit‑based)

This SDK makes it easy to connect a voice AI bot to a dTelecom room. It builds a streaming pipeline from participants’ audio: speech recognition → LLM text processing → speech synthesis, and publishes the response back to the room as an Opus track.

### Features
- Connect a bot to a dTelecom/LiveKit room via `URL + ROOM_TOKEN`.
- Processing pipeline: STT (Deepgram) → LLM (ChatGPT) → TTS (Deepgram).
- Multi‑participant support: the bot listens to participants’ microphones and replies with synthesized voice.
- Flexible extensibility via `SpeechToText`, `TextProcessor`, `TextToSpeech` interfaces and agent constructor options.

## Requirements
- Go 1.24+
- dTelecom/LiveKit account and a room token (`ROOM_TOKEN`).
- API keys:
  - `DEEPGRAM_API_KEY` — for Deepgram STT/TTS
  - `CHATGPT_API_KEY` — for the text processor (ChatGPT)

## Installation
Add the module to your project:

```bash
go get github.com/dTelecom/sdk-ai-bot
```

If your project fails to resolve Deepgram due to forked modules, add this `replace` to your project’s `go.mod`:

```go
replace github.com/deepgram/deepgram-go-sdk/v3 => github.com/dTelecom/deepgram-go-sdk/v3 v3.5.1-0.20251012194105-df6ec5cf4d79
```

This SDK already uses that `replace` internally; adding it to your app ensures consistent resolution when your build tooling vendors or overrides module graph.

## Environment variables
Note: the included examples use `godotenv` and expect a `.env` file for convenience. Your own application can source these values any way you prefer (a `.env` file is not required).

Create a `.env` file in the example directory (or your app root) or set environment variables directly:

```env
DTELECOM_URL=...          # your dTelecom server URL
ROOM_TOKEN=...            # dTelecom room token
DEEPGRAM_API_KEY=...      # Deepgram API key
CHATGPT_API_KEY=...       # OpenAI (ChatGPT) API key
```

## Quick start (connect an agent to a room)
The simplest example is in `examples/default_agent`.

Run:

```bash
cd examples/default_agent
go run .
```

Examples read the URL from the `DTELECOM_URL` env var. Set it to your own deployment.

What the example does:
1. Loads `.env` via `godotenv` (examples) and initializes the Deepgram SDK (logging).
2. Creates `agent.New(logger)` with default pipeline (Deepgram STT, ChatGPT, Deepgram TTS).
3. Calls `a.Connect(url, ROOM_TOKEN)`, publishes a local Opus track, and starts listening to participants.

## Example: agent with custom prompt
`examples/agent_with_prompt` shows how to pass your own `TextProcessor` to `agent.New` via options:

```go
textProcessor, _ := buildTextProcessor(logger) // ChatGPT with SystemPrompt
a, _ := agent.New(logger, agent.WithTextProcessor(textProcessor))
a.Connect(os.Getenv("DTELECOM_URL"), os.Getenv("ROOM_TOKEN"))
```

The `buildTextProcessor` function configures a system prompt and uses `CHATGPT_API_KEY`.

## Example: local pipeline (no LiveKit)
`examples/pipeline` demonstrates a pure local pipeline without connecting to a room: microphone → STT → ChatGPT → TTS → local playback.

Run:

```bash
cd examples/pipeline
go run .
```

## Public API

### Agent
```go
type Agent struct { /* ... */ }

func New(logger *zap.Logger, options ...Option) (*Agent, error)
func (a *Agent) Connect(url, token string) error
```

- `New` — builds the pipeline from components (Deepgram STT, ChatGPT, Deepgram TTS by default) or accepts your implementations via options.
- `Connect` — connects to the room, publishes a local Opus track, and subscribes to participants’ audio. Each participant’s audio flows through the pipeline; responses are synthesized and sent back to the room.

### Pipeline (`pkg.Pipeline`)
```go
type Pipeline struct { /* ... */ }

func NewPipeline(stt SpeechToText, tp TextProcessor, tts TextToSpeech) *Pipeline
func (p *Pipeline) Start(ctx context.Context) (<-chan AudioChunk, error)
func (p *Pipeline) AddParticipant(ctx context.Context, name string, chunks <-chan AudioChunk) error
```

- `Start` — starts processing and returns the bot’s audio chunk channel (Opus or PCM depending on TTS/transcoder).
- `AddParticipant` — adds a participant: audio stream → STT → phrase accumulation via speech start/end control tokens → questions go to `TextProcessor`.

### Interfaces for extensibility
```go
type SpeechToText interface {
    Transcribe(ctx context.Context, r <-chan AudioChunk) (<-chan SpeechChunk, error)
}

type TextProcessor interface {
    Process(ctx context.Context, question <-chan TextChunk) (<-chan TextChunk, error)
}

type TextToSpeech interface {
    Synthesize(ctx context.Context, text <-chan TextChunk) (<-chan AudioChunk, error)
}
```

Implement these interfaces to swap out Deepgram/ChatGPT for other providers. For the agent, use options:

```go
agent.WithSTT(customSTT)
agent.WithTextProcessor(customTP)
agent.WithTTS(customTTS)
```

## Running tests
The project includes unit and integration tests for STT/TTS components and utilities. Run:

```bash
go test ./...
```

Integration tests for Deepgram and transcoders may require valid API keys and audio files from `test_data`.

## License
This project is licensed under the MIT License. See the `LICENSE` file for details.


