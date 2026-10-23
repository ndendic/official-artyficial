package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/ndendic/data-start/api/envelope"
	"github.com/ndendic/data-start/api/manifest"

	"herdr-console/console"
)

// The one capability of the Manifest.
const (
	consoleID      = "artyficial.herdr.console"
	consoleVersion = "1.0.0"
)

// handler runs one action for the browser session a ticket names.
type handler func(ctx context.Context, ticket string, req console.Request) (map[string]any, *console.Fault)

// decodeInput reads the capability input strictly (unknown keys refused).
func decodeInput(raw json.RawMessage) (console.Request, *envelope.Fault) {
	var req console.Request
	if len(bytes.TrimSpace(raw)) == 0 {
		return req, &envelope.Fault{Code: envelope.Invalid, Message: `the input is an object with an "action"`}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		msg := err.Error()
		if strings.Contains(msg, "unknown field") {
			msg = "the input has a field the console does not know: " + strings.TrimPrefix(msg, "json: ")
		} else {
			msg = "the input is not a console request: " + msg
		}
		return req, &envelope.Fault{Code: envelope.Invalid, Message: msg}
	}
	if req.Action == "" {
		return req, &envelope.Fault{Code: envelope.Invalid, Message: `the input needs an "action"`}
	}
	return req, nil
}

// invoke runs one capability call and returns its result or fault.
func invoke(ctx context.Context, h handler, capability, version string, input json.RawMessage, ticket string) (json.RawMessage, *envelope.Fault) {
	if capability != consoleID || version != consoleVersion {
		return nil, &envelope.Fault{Code: envelope.NotFound, Message: "this Plugin serves only " + consoleID + " " + consoleVersion}
	}
	req, fault := decodeInput(input)
	if fault != nil {
		return nil, fault
	}
	result, f := h(ctx, ticket, req)
	if f != nil {
		code := f.Code
		if !envelope.ValidCode(code) {
			code = envelope.Internal
		}
		return nil, &envelope.Fault{Code: code, Message: f.Message}
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, &envelope.Fault{Code: envelope.Internal, Message: "result: " + err.Error()}
	}
	return raw, nil
}

// answer turns one stdio request frame into its response frame.
func answer(ctx context.Context, line []byte, h handler) manifest.WireResponse {
	var req manifest.WireRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return manifest.WireResponse{Fault: &envelope.Fault{Code: envelope.Invalid, Message: "the request is not a JSON object"}}
	}
	if req.Shutdown {
		return manifest.WireResponse{ID: req.ID}
	}
	ticket := ""
	if req.Caller != nil {
		ticket = req.Caller.Ticket
	}
	result, fault := invoke(ctx, h, req.Capability, req.Version, req.Input, ticket)
	return manifest.WireResponse{ID: req.ID, Result: result, Fault: fault}
}
