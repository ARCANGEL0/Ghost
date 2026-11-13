package agent

import (
	"context"
	"errors"
	"fmt"

	"charm.land/fantasy"
)

// autoFreeModel retries routes until one produces output.
type autoFreeModel struct {
	models []fantasy.LanguageModel
}

func (m *autoFreeModel) Generate(ctx context.Context, call fantasy.Call) (*fantasy.Response, error) {
	var errs []error
	for _, model := range m.models {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		response, err := model.Generate(ctx, call)
		if err == nil && response != nil {
			return response, nil
		}
		if err == nil {
			err = errors.New("provider returned an empty response")
		}
		errs = append(errs, fmt.Errorf("%s/%s: %w", model.Provider(), model.Model(), err))
	}
	return nil, autoFreeError(errs)
}

func (m *autoFreeModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	if len(m.models) == 0 {
		return nil, autoFreeError(nil)
	}
	return func(yield func(fantasy.StreamPart) bool) {
		var errs []error
		for _, model := range m.models {
			if err := ctx.Err(); err != nil {
				yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeError, Error: err})
				return
			}
			stream, err := model.Stream(ctx, call)
			if err != nil {
				if ctx.Err() != nil {
					yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeError, Error: ctx.Err()})
					return
				}
				errs = append(errs, fmt.Errorf("%s/%s: %w", model.Provider(), model.Model(), err))
				continue
			}
			if stream == nil {
				errs = append(errs, fmt.Errorf("%s/%s: provider returned an empty stream", model.Provider(), model.Model()))
				continue
			}

			var pending []fantasy.StreamPart
			var firstErr error
			started := false
			for part := range stream {
				if part.Type == fantasy.StreamPartTypeError {
					if part.Error == nil {
						part.Error = errors.New("provider stream failed")
					}
					if !started {
						firstErr = part.Error
						break
					}
					yield(part)
					return
				}

				if !started && !autoFreeHasOutput(part) {
					pending = append(pending, part)
					continue
				}
				if autoFreeHasOutput(part) {
					started = true
					for _, saved := range pending {
						if !yield(saved) {
							return
						}
					}
					pending = nil
				}
				if !yield(part) {
					return
				}
			}
			if firstErr != nil {
				if ctx.Err() != nil {
					yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeError, Error: ctx.Err()})
					return
				}
				errs = append(errs, fmt.Errorf("%s/%s: %w", model.Provider(), model.Model(), firstErr))
				continue
			}
			for _, part := range pending {
				if !yield(part) {
					return
				}
			}
			return
		}
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeError, Error: autoFreeError(errs)})
	}, nil
}

func (m *autoFreeModel) GenerateObject(ctx context.Context, call fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	var errs []error
	for _, model := range m.models {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		response, err := model.GenerateObject(ctx, call)
		if err == nil && response != nil {
			return response, nil
		}
		if err == nil {
			err = errors.New("provider returned an empty response")
		}
		errs = append(errs, fmt.Errorf("%s/%s: %w", model.Provider(), model.Model(), err))
	}
	return nil, autoFreeError(errs)
}

func (m *autoFreeModel) StreamObject(ctx context.Context, call fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	if len(m.models) == 0 {
		return nil, autoFreeError(nil)
	}
	return func(yield func(fantasy.ObjectStreamPart) bool) {
		var errs []error
		for _, model := range m.models {
			if err := ctx.Err(); err != nil {
				yield(fantasy.ObjectStreamPart{Type: fantasy.ObjectStreamPartTypeError, Error: err})
				return
			}
			stream, err := model.StreamObject(ctx, call)
			if err != nil {
				if ctx.Err() != nil {
					yield(fantasy.ObjectStreamPart{Type: fantasy.ObjectStreamPartTypeError, Error: ctx.Err()})
					return
				}
				errs = append(errs, fmt.Errorf("%s/%s: %w", model.Provider(), model.Model(), err))
				continue
			}
			if stream == nil {
				errs = append(errs, fmt.Errorf("%s/%s: provider returned an empty stream", model.Provider(), model.Model()))
				continue
			}

			var pending []fantasy.ObjectStreamPart
			var firstErr error
			started := false
			for part := range stream {
				if part.Type == fantasy.ObjectStreamPartTypeError {
					if part.Error == nil {
						part.Error = errors.New("provider stream failed")
					}
					if !started {
						firstErr = part.Error
						break
					}
					yield(part)
					return
				}

				if !started && !autoFreeObjectHasOutput(part) {
					pending = append(pending, part)
					continue
				}
				if autoFreeObjectHasOutput(part) {
					started = true
					for _, saved := range pending {
						if !yield(saved) {
							return
						}
					}
					pending = nil
				}
				if !yield(part) {
					return
				}
			}
			if firstErr != nil {
				if ctx.Err() != nil {
					yield(fantasy.ObjectStreamPart{Type: fantasy.ObjectStreamPartTypeError, Error: ctx.Err()})
					return
				}
				errs = append(errs, fmt.Errorf("%s/%s: %w", model.Provider(), model.Model(), firstErr))
				continue
			}
			for _, part := range pending {
				if !yield(part) {
					return
				}
			}
			return
		}
		yield(fantasy.ObjectStreamPart{Type: fantasy.ObjectStreamPartTypeError, Error: autoFreeError(errs)})
	}, nil
}

func (m *autoFreeModel) Provider() string { return "auto-free" }

func (m *autoFreeModel) Model() string { return "auto:free" }

func autoFreeHasOutput(part fantasy.StreamPart) bool {
	switch part.Type {
	case fantasy.StreamPartTypeTextDelta,
		fantasy.StreamPartTypeReasoningDelta,
		fantasy.StreamPartTypeToolInputStart,
		fantasy.StreamPartTypeToolInputDelta,
		fantasy.StreamPartTypeToolCall,
		fantasy.StreamPartTypeToolResult,
		fantasy.StreamPartTypeSource:
		return true
	default:
		return false
	}
}

func autoFreeObjectHasOutput(part fantasy.ObjectStreamPart) bool {
	return part.Type == fantasy.ObjectStreamPartTypeTextDelta ||
		(part.Type == fantasy.ObjectStreamPartTypeObject && part.Object != nil)
}

func autoFreeError(errs []error) error {
	if len(errs) == 0 {
		return errors.New("AUTO:FREE has no available free routes")
	}
	return fmt.Errorf("all AUTO:FREE routes failed: %w", errors.Join(errs...))
}
