package service

import "testing"

// TestVoidedBaselineFallbackWithAPIOrderTimestamps exercises the exact
// acceptance order produced by the API (CURRENT_TIMESTAMP, no timestamp
// manipulation): tied acceptance instants must still fall back by ID.
func TestVoidedBaselineFallbackWithAPIOrderTimestamps(t *testing.T) {
	fixture := newBaselineFixture(t)
	pa := fixture.createProgram(t, "LIVE", 1, trajectoryClean, simpleInterlocks())
	ra := fixture.runSimulation(t, pa.ID, "live-key-1")
	fixture.reviewAndAccept(t, ra.ID)
	pb := fixture.createProgram(t, "LIVE", 2, trajectoryClean, simpleInterlocks())
	rb := fixture.runSimulation(t, pb.ID, "live-key-2")
	fixture.reviewAndAccept(t, rb.ID)
	pc := fixture.createProgram(t, "LIVE", 3, trajectoryGate, simpleInterlocks())
	rc := fixture.runSimulation(t, pc.ID, "live-key-3")
	if rc.BaselineRunID == nil || *rc.BaselineRunID != rb.ID {
		t.Fatalf("C should bind B=%d, got %v", rb.ID, rc.BaselineRunID)
	}
	if _, err := fixture.service.Void(rb.ID, "withdraw B", fixture.reviewer, "v"); err != nil {
		t.Fatal(err)
	}
	got, _ := fixture.service.Get(rc.ID)
	if got.BaselineRunID == nil || *got.BaselineRunID != ra.ID {
		t.Fatalf("after void B, C should fall back to A=%d, got %v", ra.ID, got.BaselineRunID)
	}
}
