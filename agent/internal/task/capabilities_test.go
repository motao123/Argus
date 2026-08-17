package task

import (
	"encoding/json"
	"testing"

	"github.com/motao123/Argus/protocol"
)

func TestCapabilityGate(t *testing.T) {
	h := NewHandler(nil)
	h.SetCapabilities(protocol.Capabilities{})
	_, rpcErr := h.Handle(protocol.MethodExec, json.RawMessage(`{}`))
	if rpcErr == nil || rpcErr.Code != protocol.ErrCapabilityDisabled {
		t.Fatalf("gate error = %#v", rpcErr)
	}
}
