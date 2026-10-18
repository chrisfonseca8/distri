package services

import (
	"context"
	"testing"
	"uuid"

	"github.com/chrisfonseca08/distri/payment-service/pkg/payment_gateway"
)

func TestPaymentResultUpdates(t *testing.T) {
	tests := []struct {
		name        string
		ref         string
		err         error
		wantStatus  string
		wantFailure any
		wantRef     any
	}{
		{
			name:        "success",
			ref:         "sim_order_123",
			wantStatus:  "SUCCEEDED",
			wantFailure: nil,
			wantRef:     "sim_order_123",
		},
		{
			name: "decline",
			err: &paymentgateway.ErrDeclined{
				OrderID: uuid.MustParse("70000000-0000-4000-8000-000000000001"),
			},
			wantStatus:  "FAILED",
			wantFailure: "DECLINED",
		},
		{
			name:        "ambiguous timeout",
			err:         context.DeadlineExceeded,
			wantStatus:  "UNKNOWN",
			wantFailure: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			updates := paymentResultUpdates(test.ref, test.err)
			if updates["status"] != test.wantStatus {
				t.Fatalf("status = %v, want %s", updates["status"], test.wantStatus)
			}
			if updates["failure_code"] != test.wantFailure {
				t.Fatalf("failure_code = %v, want %v", updates["failure_code"], test.wantFailure)
			}
			if test.wantRef != nil && updates["gateway_order_id"] != test.wantRef {
				t.Fatalf("gateway_order_id = %v, want %v", updates["gateway_order_id"], test.wantRef)
			}
		})
	}
}
