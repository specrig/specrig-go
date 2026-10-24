package service

import (
	"context"

	"example.test/domain/invoice/model"
)

type Invoices struct{}

func NewInvoices() *Invoices { return &Invoices{} }

func (Invoices) Issue(_ context.Context, amount int) (*model.Invoice, error) {
	return &model.Invoice{Amount: amount}, nil
}

func (Invoices) Cancel(_ context.Context, id int) error { return nil }
