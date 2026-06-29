package voucher

import "github.com/lallene/medcore-his/backend/internal/core/workflow"

var VoucherWorkflow = workflow.Definition{
	Name:       "insurance_voucher",
	EntityName: "InsuranceVoucher",
	Initial:    "draft",
	Transitions: []workflow.Transition{
		{
			Action:     "submit",
			From:       []workflow.State{"draft"},
			To:         "submitted",
			Permission: "insurance.voucher.submit",
		},
		{
			Action:     "control",
			From:       []workflow.State{"submitted"},
			To:         "controlled",
			Permission: "insurance.voucher.control",
		},
		{
			Action:     "validate",
			From:       []workflow.State{"controlled"},
			To:         "validated",
			Permission: "insurance.voucher.validate",
		},
		{
			Action:     "reject",
			From:       []workflow.State{"submitted", "controlled"},
			To:         "rejected",
			Permission: "insurance.voucher.reject",
		},
		{
			Action:     "cancel",
			From:       []workflow.State{"draft", "submitted", "controlled"},
			To:         "cancelled",
			Permission: "insurance.voucher.cancel",
		},
	},
}
