package ai

import (
	"testing"

	piai "github.com/HycJack/pi-ai-go"
)

// Every protocol a model is built for has a provider to speak it, registered
// by the import of pi-ai-go's providers: without one, a question ends in
// "no provider registered for API", with nothing said of the model.
func TestEveryProtocolHasAProvider(t *testing.T) {
	for _, protocol := range Protocols() {
		m, err := Target(protocol, "", "a-model")
		if err != nil {
			t.Fatalf("%s: %v", protocol, err)
		}
		if _, err := piai.GetProvider(m.API); err != nil {
			t.Errorf("a model of %s has no provider: %v", protocol, err)
		}
	}
}
