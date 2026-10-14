package dashboard

import "testing"

func TestDecideOrderProgression(t *testing.T) {
	base := DeviceUpdate{OrderID: "ord-42", DeviceID: "pos-7", AccountID: "shop-9", Stage: StageCheckout, Status: "paid", Sequence: 1}
	tests := []struct {
		name     string
		previous *DeviceUpdate
		next     DeviceUpdate
		accepted bool
	}{
		{name: "checkout opens the order", next: base, accepted: true},
		{name: "fulfillment follows checkout", previous: &base, next: DeviceUpdate{OrderID: "ord-42", DeviceID: "pos-7", AccountID: "shop-9", Stage: StageFulfillment, Status: "packed", Sequence: 2}, accepted: true},
		{name: "receipt cannot skip fulfillment", previous: &base, next: DeviceUpdate{OrderID: "ord-42", DeviceID: "pos-7", AccountID: "shop-9", Stage: StageReceipt, Status: "issued", Sequence: 2}},
		{name: "duplicate sequence is rejected", previous: &base, next: DeviceUpdate{OrderID: "ord-42", DeviceID: "pos-7", AccountID: "shop-9", Stage: StageFulfillment, Status: "packed", Sequence: 1}},
		{name: "identity is stable", previous: &base, next: DeviceUpdate{OrderID: "ord-42", DeviceID: "pos-8", AccountID: "shop-9", Stage: StageFulfillment, Status: "packed", Sequence: 2}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := Decide(test.previous, test.next)
			if decision.Accepted != test.accepted {
				t.Fatalf("Accepted = %v, want %v; reason=%q", decision.Accepted, test.accepted, decision.Reason)
			}
		})
	}
}
