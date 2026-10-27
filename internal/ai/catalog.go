package ai

import (
	"fmt"
	"sort"

	piai "github.com/HycJack/pi-ai-go"
)

// Providers lists, alphabetically, the providers that offer chat models.
func Providers() []string {
	var out []string
	for _, p := range piai.GetProviders() {
		if len(piai.GetModels(p)) > 0 {
			out = append(out, string(p))
		}
	}
	sort.Strings(out)
	return out
}

// DefaultProvider is the provider a settings dialog starts on: the first a
// key was given for, or else the first that offers models.
func DefaultProvider(keys Keys) string {
	ps := Providers()
	for _, name := range ps {
		if keys[name] != "" {
			return name
		}
	}
	if len(ps) > 0 {
		return ps[0]
	}
	return ""
}

// Models lists the chat models of a provider.
func Models(provider string) []piai.Model {
	return piai.GetModels(piai.KnownProvider(provider))
}

// Resolve is the model that provider offers as model.
func Resolve(provider, model string) (piai.Model, error) {
	m, err := piai.GetModel(piai.KnownProvider(provider), model)
	if err != nil {
		return piai.Model{}, fmt.Errorf("ai: %w", err)
	}
	return m, nil
}
