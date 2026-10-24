package adapter

import (
	"context"

	invoiceModel "example.test/domain/invoice/model"
)

type Billing interface {
	Issue(ctx context.Context, amount int) (*invoiceModel.Invoice, error)
}

type billing struct{ billing Billing }

func (b *billing) Bill(ctx context.Context, amount int) error {
	_, err := b.billing.Issue(ctx, amount)
	return err
}
