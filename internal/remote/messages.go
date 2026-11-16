package remote

import (
	"encoding/json"
	"time"

	"github.com/swadhinbiswas/ghost/internal/message"
)

func convertMessages(items []message.Message) []apiMessage {
	results := make(map[string]message.ToolResult)
	for _, item := range items {
		for _, result := range item.ToolResults() {
			results[result.ToolCallID] = result
		}
	}

	data := make([]apiMessage, 0, len(items))
	for _, item := range items {
		converted := convertMessage(item)
		if item.Role == message.Tool {
			continue
		}
		if item.Role == message.Assistant {
			for i := range converted.Content {
				if converted.Content[i].Type != "tool" || converted.Content[i].State == nil {
					continue
				}
				result, ok := results[converted.Content[i].ID]
				if !ok {
					continue
				}
				if result.IsError {
					converted.Content[i].State.Status = "error"
					converted.Content[i].State.Error = &errorView{Message: result.Content}
				} else {
					converted.Content[i].State.Status = "completed"
				}
			}
		}
		data = append(data, converted)
		if item.Role == message.Assistant && item.FinishPart() != nil {
			outcome := "completed"
			switch item.FinishReason() {
			case message.FinishReasonCanceled:
				outcome = "interrupted"
			case message.FinishReasonError, message.FinishReasonPermissionDenied:
				outcome = "failed"
			}
			data = append(data, apiMessage{
				ID:      item.ID + "-idle",
				Type:    "idle",
				Outcome: outcome,
				Time:    &messageTime{Created: unixMillis(item.FinishPart().Time)},
			})
		}
	}
	return data
}

func convertMessage(item message.Message) apiMessage {
	created := unixMillis(item.CreatedAt)
	converted := apiMessage{
		ID:   item.ID,
		Time: &messageTime{Created: created},
	}
	switch item.Role {
	case message.User:
		converted.Type = "user"
		converted.Text = item.Content().Text
	case message.System:
		converted.Type = "system"
		converted.Text = item.Content().Text
	case message.Assistant:
		converted.Type = "assistant"
		for _, part := range item.Parts {
			switch part := part.(type) {
			case message.TextContent:
				converted.Content = append(converted.Content, apiPart{Type: "text", Text: part.Text})
			case message.ToolCall:
				status := "pending"
				if part.Finished {
					status = "running"
				}
				var input any = map[string]any{}
				if part.Input != "" {
					if err := json.Unmarshal([]byte(part.Input), &input); err != nil {
						input = part.Input
					}
				}
				converted.Content = append(converted.Content, apiPart{
					Type: "tool",
					ID:   part.ID,
					Name: part.Name,
					State: &toolState{
						Input:  input,
						Status: status,
					},
				})
			}
		}
	default:
		return apiMessage{}
	}
	return converted
}

func reverseMessages(items []apiMessage) {
	for left, right := 0, len(items)-1; left < right; left, right = left+1, right-1 {
		items[left], items[right] = items[right], items[left]
	}
}

func unixMillis(seconds int64) int64 {
	if seconds == 0 {
		return time.Now().UnixMilli()
	}
	return time.Unix(seconds, 0).UnixMilli()
}
