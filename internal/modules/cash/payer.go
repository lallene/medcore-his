package cash

import "github.com/lallene/medcore-his/backend/internal/modules/billing"

func toBillingPayer(p *PayerRequest) *billing.PayerRequest {
	if p == nil {
		return nil
	}
	return &billing.PayerRequest{
		Mode:         p.Mode,
		PartyID:      p.PartyID,
		DisplayName:  p.DisplayName,
		Phone:        p.Phone,
		Relationship: p.Relationship,
	}
}
