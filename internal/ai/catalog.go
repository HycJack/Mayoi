package ai

import (
	"strings"

	piai "github.com/HycJack/pi-ai-go"
)

// The two ways an endpoint can speak. Anything that speaks one of them
// works: OpenAI's own service, Anthropic's own service, and the many
// servers laid out like them.
const (
	OpenAI    = "openai"
	Anthropic = "anthropic"
)

// Protocols lists the protocols, in the order settings offers them.
func Protocols() []string {
	return []string{OpenAI, Anthropic}
}

// DisplayName is what a protocol is called where someone reads it.
func DisplayName(protocol string) string {
	if protocol == Anthropic {
		return "Anthropic"
	}
	return "OpenAI"
}

// DefaultBaseURL is where the service of a protocol lives, which an empty
// base URL means: the place the protocol's own paths start from, which is
// what pi-ai-go appends them to.
func DefaultBaseURL(protocol string) string {
	if protocol == Anthropic {
		return "https://api.anthropic.com/v1"
	}
	return "https://api.openai.com/v1"
}

// baseOf is the base URL in use: the one given, or the protocol's own.
func baseOf(protocol, baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return DefaultBaseURL(protocol)
	}
	return baseURL
}

// ModelsURL is where an endpoint lists the models it offers. A base URL
// someone gives is the place the protocol's own paths start from, which is
// what makes a proxy work: only the services Mayoi knows are told the /v1
// their own listing needs.
func ModelsURL(protocol, baseURL string) string {
	return baseOf(protocol, baseURL) + "/models"
}

// The sizes a model is taken to have when nothing is known about it: a
// model someone typed is one Mayoi has no description of, and the loop
// needs a window to measure a conversation against.
const (
	modelWindow = 200_000
	modelOutput = 8_192
)

// Target is the model to ask, over the protocol, at the base URL given.
func Target(protocol, baseURL, model string) (piai.Model, error) {
	if protocol != OpenAI && protocol != Anthropic {
		return piai.Model{}, &UnknownProtocol{Protocol: protocol}
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return piai.Model{}, &NoModel{}
	}
	m := piai.Model{
		ID:            model,
		Name:          model,
		Provider:      piai.KnownProvider(protocol),
		BaseURL:       baseOf(protocol, baseURL),
		API:           piai.APIOpenAICompletions,
		Input:         []piai.Modality{piai.ModalityText},
		ContextWindow: modelWindow,
		MaxTokens:     modelOutput,
	}
	if protocol == Anthropic {
		m.API = piai.APIAnthropicMessages
	}
	return m, nil
}

// UnknownProtocol is a protocol neither of the two.
type UnknownProtocol struct{ Protocol string }

func (e *UnknownProtocol) Error() string { return "unknown protocol " + e.Protocol }

// NoModel is settings that name no model.
type NoModel struct{}

func (e *NoModel) Error() string { return "no model chosen" }

// ProtocolOf is the protocol a key was given for, which is the one to use
// when settings name none: the first that has a key.
func ProtocolOf(keys Keys) string {
	for _, name := range Protocols() {
		if keys[name] != "" {
			return name
		}
	}
	return OpenAI
}
