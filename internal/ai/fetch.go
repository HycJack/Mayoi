package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// fetchTimeout is how long listing the models of an endpoint may take.
const fetchTimeout = 20 * time.Second

// maxListedModels caps what is read of a list, which no endpoint has ever
// come close to.
const maxListedModels = 4 << 20

// FetchModels asks an endpoint which models it offers. A server on this
// machine often wants no key, so one that is empty is not sent.
func FetchModels(ctx context.Context, protocol, baseURL, key string) ([]string, error) {
	url := ModelsURL(protocol, baseURL)
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if key = strings.TrimSpace(key); key != "" {
		if protocol == Anthropic {
			req.Header.Set("x-api-key", key)
			req.Header.Set("anthropic-version", "2023-06-01")
		} else {
			req.Header.Set("Authorization", "Bearer "+key)
		}
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ai: %s: %w", url, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, maxListedModels))
	if err != nil {
		return nil, fmt.Errorf("ai: %s: %w", url, err)
	}
	if res.StatusCode != http.StatusOK {
		// The reason is in the body, which is where a wrong key says so.
		return nil, &FetchError{URL: url, Status: res.Status, Reason: firstLine(body)}
	}
	// Both services list models the same way: data[].id.
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, &FetchError{URL: url, Status: res.Status, Reason: firstLine(body)}
	}
	seen := make(map[string]bool, len(list.Data))
	out := make([]string, 0, len(list.Data))
	for _, m := range list.Data {
		id := strings.TrimSpace(m.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil, &FetchError{URL: url, Status: res.Status, Reason: "no models in the answer"}
	}
	return out, nil
}

// FetchError is an endpoint that would not say what it offers.
type FetchError struct {
	URL    string
	Status string
	// Reason is the first line of what it said, or what was wrong with it.
	Reason string
}

func (e *FetchError) Error() string {
	if e.Reason == "" {
		return "ai: " + e.URL + " answered " + e.Status
	}
	return "ai: " + e.URL + " answered " + e.Status + ": " + e.Reason
}

// firstLine is what a body says, on one line, short enough to show.
func firstLine(body []byte) string {
	s := string(body)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		// Cut on a rune, which a status message has a few of.
		s = strings.ToValidUTF8(s[:200], "") + "…"
	}
	return s
}
