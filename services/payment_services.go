package services

import (
	"github.com/midtrans/midtrans-go"
	"github.com/midtrans/midtrans-go/snap"
)

type PaymentService struct{}

// CreatePayment handles the creation of a payment request using Snap
func (service *PaymentService) CreatePayment(orderID string, grossAmount int64) (*snap.Response, error) {
	return service.CreatePaymentWithCallback(orderID, grossAmount, "https://hand.tbn1.site/appointment-history")
}

// CreatePaymentWithCallback allows customizing the finish URL for the payment.
func (service *PaymentService) CreatePaymentWithCallback(orderID string, grossAmount int64, finishURL string) (*snap.Response, error) {
	if finishURL == "" {
		finishURL = "https://hand.tbn1.site/appointment-history"
	}

	req := &snap.Request{
		TransactionDetails: midtrans.TransactionDetails{
			OrderID:  orderID,
			GrossAmt: grossAmount,
		},
		Expiry: &snap.ExpiryDetails{
			Unit:     "minute",
			Duration: 5,
		},
		Callbacks: &snap.Callbacks{
			Finish: finishURL,
		},
	}

	// Create transaction using the globally set ServerKey and Environment
	resp, err := snap.CreateTransaction(req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}
