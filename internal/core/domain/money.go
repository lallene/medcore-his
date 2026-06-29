package domain

import "fmt"

type Money struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

func NewMoney(amount float64, currency string) (Money, error) {
	if amount < 0 {
		return Money{}, fmt.Errorf("le montant ne peut pas être négatif")
	}

	if currency == "" {
		currency = "XOF"
	}

	return Money{
		Amount:   amount,
		Currency: currency,
	}, nil
}

func (m Money) IsValid() bool {
	return m.Amount >= 0 && m.Currency != ""
}
