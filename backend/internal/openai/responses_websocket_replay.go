package openai

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

func responsesWebSocketInputItems(frame []byte) ([]json.RawMessage, bool, bool, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(frame, &payload); err != nil {
		return nil, false, false, err
	}
	raw, exists := payload["input"]
	if !exists {
		return nil, false, true, nil
	}
	trimmed := bytes.TrimSpace(raw)
	if bytes.Equal(trimmed, []byte("null")) || len(trimmed) == 0 {
		return nil, true, true, nil
	}
	switch trimmed[0] {
	case '[':
		var items []json.RawMessage
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return nil, false, false, err
		}
		return items, true, true, nil
	case '{':
		return []json.RawMessage{append(json.RawMessage(nil), trimmed...)}, true, true, nil
	case '"':
		var content string
		if err := json.Unmarshal(trimmed, &content); err != nil {
			return nil, false, false, err
		}
		if strings.TrimSpace(content) == "" {
			return []json.RawMessage{}, true, true, nil
		}
		message, err := json.Marshal(map[string]any{"type": "message", "role": "user", "content": content})
		if err != nil {
			return nil, false, false, err
		}
		return []json.RawMessage{message}, true, true, nil
	default:
		// Preserve the existing upstream contract for unrecognized input shapes,
		// but disable cross-account replay because the conversation cannot be
		// reconstructed without changing that shape.
		return nil, true, false, nil
	}
}

const (
	responsesWebSocketReplayHistoryMaxItems = 4096
	responsesWebSocketReplayHistoryMaxBytes = 4 << 20
)

type responsesWebSocketReplayHistory struct {
	responseID  string
	items       []json.RawMessage
	bytes       int64
	contexts    map[string]struct{}
	valid       bool
	reservation responsesWebSocketReplayReservation
}

func newResponsesWebSocketReplayHistory(budget *responsesWebSocketReplayBudget) responsesWebSocketReplayHistory {
	return responsesWebSocketReplayHistory{reservation: newResponsesWebSocketReplayReservation(budget)}
}

type responsesWebSocketReplayPlan struct {
	useHistory         bool
	replaceWithCurrent bool
	currentFrom        int
	itemCount          int
	bytes              int64
}

func (h *responsesWebSocketReplayHistory) plan(
	previousResponseID string,
	current []json.RawMessage,
	currentExists bool,
	currentReplayable bool,
) (responsesWebSocketReplayPlan, bool, bool) {
	previousResponseID = strings.TrimSpace(previousResponseID)
	currentBytes := responsesWebSocketRawMessagesBytes(current)
	if previousResponseID == "" {
		plan := responsesWebSocketReplayPlan{
			replaceWithCurrent: true,
			itemCount:          len(current),
			bytes:              currentBytes,
		}
		// An independent response.create can be replayed byte-for-byte even when
		// its input shape cannot be normalized into durable conversation history.
		retrySafe := true
		if currentReplayable {
			_, retrySafe = responsesWebSocketToolContextDelta(nil, current)
		}
		return plan, retrySafe, currentReplayable && retrySafe
	}
	if h == nil || !h.valid || !currentReplayable || previousResponseID != h.responseID {
		return responsesWebSocketReplayPlan{}, false, false
	}

	plan := responsesWebSocketReplayPlan{useHistory: true, itemCount: len(h.items) + len(current), bytes: h.bytes + currentBytes}
	newItems := current
	if currentExists && len(current) > 0 && responsesWebSocketRawPrefix(current, h.items) {
		plan.replaceWithCurrent = true
		plan.currentFrom = len(h.items)
		plan.itemCount = len(current)
		plan.bytes = currentBytes
		newItems = current[plan.currentFrom:]
	}
	if responsesWebSocketReplayLimitExceeded(plan.itemCount, plan.bytes) {
		return plan, false, false
	}
	if _, complete := responsesWebSocketToolContextDelta(h.contexts, newItems); !complete {
		return plan, false, false
	}
	return plan, true, true
}

func (p responsesWebSocketReplayPlan) input(history, current []json.RawMessage) []json.RawMessage {
	if !p.useHistory || p.replaceWithCurrent {
		return current
	}
	if len(current) == 0 {
		return history
	}
	merged := make([]json.RawMessage, 0, len(history)+len(current))
	merged = append(merged, history...)
	merged = append(merged, current...)
	return merged
}

func (h *responsesWebSocketReplayHistory) commit(
	responseID string,
	plan responsesWebSocketReplayPlan,
	current, output []json.RawMessage,
	replayable bool,
	currentReservation, outputReservation *responsesWebSocketReplayReservation,
) {
	reject := func() {
		if h != nil {
			h.invalidate()
		}
		if currentReservation != nil {
			currentReservation.release()
		}
		if outputReservation != nil {
			outputReservation.release()
		}
	}
	responseID = strings.TrimSpace(responseID)
	if h == nil || !replayable || responseID == "" {
		reject()
		return
	}
	itemCount := plan.itemCount + len(output)
	totalBytes := plan.bytes + responsesWebSocketRawMessagesBytes(output)
	if totalBytes < 0 || responsesWebSocketReplayLimitExceeded(itemCount, totalBytes) {
		reject()
		return
	}

	newItems := current
	existingContexts := map[string]struct{}(nil)
	if plan.useHistory {
		existingContexts = h.contexts
		if plan.replaceWithCurrent {
			newItems = current[plan.currentFrom:]
		} else {
			newItems = current
		}
	}
	addedContexts, complete := responsesWebSocketToolContextDelta(existingContexts, newItems, output)
	if !complete {
		reject()
		return
	}
	if !h.reservation.replaceWith(totalBytes, currentReservation, outputReservation) {
		reject()
		return
	}

	switch {
	case !plan.useHistory:
		h.items = current
		h.contexts = nil
	case plan.replaceWithCurrent:
		h.items = current
	default:
		h.items = append(h.items, current...)
	}
	h.items = append(h.items, output...)
	if h.contexts == nil {
		h.contexts = make(map[string]struct{}, len(addedContexts))
	}
	for callID := range addedContexts {
		h.contexts[callID] = struct{}{}
	}
	h.responseID = responseID
	h.bytes = totalBytes
	h.valid = true
}

func (h *responsesWebSocketReplayHistory) invalidate() {
	if h == nil {
		return
	}
	h.responseID = ""
	h.items = nil
	h.bytes = 0
	h.contexts = nil
	h.valid = false
	h.reservation.release()
}

func responsesWebSocketReplayLimitExceeded(items int, bytes int64) bool {
	return items > responsesWebSocketReplayHistoryMaxItems || bytes > responsesWebSocketReplayHistoryMaxBytes
}

func responsesWebSocketRawMessagesBytes(items []json.RawMessage) int64 {
	var total int64
	for _, item := range items {
		total += int64(len(item))
	}
	return total
}

func buildResponsesWebSocketRetryFrame(frame []byte, fullInput []json.RawMessage) ([]byte, bool, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(frame, &payload); err != nil {
		return nil, false, err
	}
	input, err := json.Marshal(fullInput)
	if err != nil {
		return nil, false, err
	}
	payload["input"] = input
	delete(payload, "previous_response_id")
	if !responsesWebSocketToolContextComplete(fullInput) {
		return nil, false, nil
	}
	rebuilt, err := json.Marshal(payload)
	return rebuilt, err == nil, err
}

func responsesWebSocketToolContextComplete(items []json.RawMessage) bool {
	_, complete := responsesWebSocketToolContextDelta(nil, items)
	return complete
}

func responsesWebSocketToolContextDelta(existing map[string]struct{}, groups ...[]json.RawMessage) (map[string]struct{}, bool) {
	contexts := make(map[string]struct{})
	requiredCallIDs := make([]string, 0)
	for _, items := range groups {
		for _, raw := range items {
			var item map[string]any
			if json.Unmarshal(raw, &item) != nil {
				continue
			}
			itemType, _ := item["type"].(string)
			itemType = strings.TrimSpace(itemType)
			callID, _ := item["call_id"].(string)
			callID = strings.TrimSpace(callID)
			switch itemType {
			case "tool_call", "function_call", "local_shell_call", "tool_search_call", "custom_tool_call", "mcp_tool_call":
				if callID != "" {
					contexts[callID] = struct{}{}
				}
			case "item_reference":
				// An item_reference requires account- or connection-scoped upstream
				// state. Keeping the reference in a replay frame would still make the
				// replacement account resolve that remote state, even if a sibling
				// concrete item happens to carry the same identifier.
				return nil, false
			case "function_call_output", "local_shell_call_output", "tool_search_output", "custom_tool_call_output", "mcp_tool_call_output":
				if callID == "" {
					return nil, false
				}
				requiredCallIDs = append(requiredCallIDs, callID)
			default:
				// New Responses tool outputs are account-scoped until their concrete
				// call type is explicitly supported above. Failing closed prevents an
				// unrecognized orphan output from being replayed to another account.
				if strings.HasSuffix(itemType, "_call_output") {
					return nil, false
				}
			}
		}
	}
	for _, contextID := range requiredCallIDs {
		if _, exists := contexts[contextID]; exists {
			continue
		}
		if _, exists := existing[contextID]; !exists {
			return nil, false
		}
	}
	return contexts, true
}

func responsesWebSocketRawPrefix(items, prefix []json.RawMessage) bool {
	if len(prefix) > len(items) {
		return false
	}
	for index := range prefix {
		item := bytes.TrimSpace(items[index])
		candidate := bytes.TrimSpace(prefix[index])
		if bytes.Equal(item, candidate) {
			continue
		}
		canonicalItem, itemOK := canonicalResponsesWebSocketJSON(item)
		canonicalCandidate, candidateOK := canonicalResponsesWebSocketJSON(candidate)
		if !itemOK || !candidateOK || !bytes.Equal(canonicalItem, canonicalCandidate) {
			return false
		}
	}
	return true
}

func canonicalResponsesWebSocketJSON(raw []byte) ([]byte, bool) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return nil, false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, false
	}
	canonical, err := json.Marshal(value)
	return canonical, err == nil
}

type responsesWebSocketReplayCollector struct {
	items        []json.RawMessage
	seen         map[string]struct{}
	bytes        int64
	exceedsLimit bool
	disabled     bool
	reservation  *responsesWebSocketReplayReservation
}

func (c *responsesWebSocketReplayCollector) addEvent(eventType string, frame []byte) {
	if c == nil || c.disabled {
		return
	}
	var event map[string]json.RawMessage
	if json.Unmarshal(frame, &event) != nil {
		return
	}
	switch strings.TrimSpace(eventType) {
	case "response.output_item.done":
		c.addItem(event["item"])
	case "response.completed", "response.done":
		var response map[string]json.RawMessage
		if json.Unmarshal(event["response"], &response) != nil {
			return
		}
		var output []json.RawMessage
		if json.Unmarshal(response["output"], &output) != nil {
			return
		}
		for _, item := range output {
			c.addItem(item)
		}
	}
}

func (c *responsesWebSocketReplayCollector) addItem(raw json.RawMessage) {
	if c == nil || c.disabled {
		return
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return
	}
	var item struct {
		Type   string `json:"type"`
		ID     string `json:"id"`
		CallID string `json:"call_id"`
	}
	if json.Unmarshal(trimmed, &item) != nil {
		return
	}
	if strings.TrimSpace(item.Type) == "" {
		return
	}
	key := item.ID
	if strings.TrimSpace(key) == "" {
		key = item.CallID
	}
	if strings.TrimSpace(key) == "" {
		digest := sha256.Sum256(trimmed)
		key = "raw:" + string(digest[:])
	} else {
		key = "id:" + strings.TrimSpace(key)
	}
	if c.seen == nil {
		c.seen = make(map[string]struct{})
	}
	if _, exists := c.seen[key]; exists {
		return
	}
	nextBytes := c.bytes + int64(len(trimmed))
	if nextBytes < c.bytes || responsesWebSocketReplayLimitExceeded(len(c.items)+1, nextBytes) {
		c.markExceededLimit()
		return
	}
	if c.reservation != nil && !c.reservation.resize(nextBytes) {
		c.markExceededLimit()
		return
	}
	c.seen[key] = struct{}{}
	c.items = append(c.items, append(json.RawMessage(nil), trimmed...))
	c.bytes = nextBytes
}

func (c *responsesWebSocketReplayCollector) markExceededLimit() {
	if c == nil {
		return
	}
	c.disable()
	c.exceedsLimit = true
}

func (c *responsesWebSocketReplayCollector) disable() {
	if c == nil {
		return
	}
	c.release()
	c.disabled = true
}

func (c *responsesWebSocketReplayCollector) release() {
	if c == nil {
		return
	}
	if c.reservation != nil {
		c.reservation.release()
	}
	c.items = nil
	c.seen = nil
	c.bytes = 0
}
