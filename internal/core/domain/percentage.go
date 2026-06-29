package domain

import "fmt"

type Percentage struct {
	Value float64 `json:"value"`
}

func NewPercentage(value float64) (Percentage, error) {
	if value < 0 || value > 100 {
		return Percentage{}, fmt.Errorf("le pourcentage doit être compris entre 0 et 100")
	}

	return Percentage{Value: value}, nil
}

func (p Percentage) IsValid() bool {
	return p.Value >= 0 && p.Value <= 100
}
