package payment

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

func NewOrderID() (string, error) {
	limit := new(big.Int).Exp(big.NewInt(10), big.NewInt(30), nil)
	number, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("20%030d", number), nil
}
