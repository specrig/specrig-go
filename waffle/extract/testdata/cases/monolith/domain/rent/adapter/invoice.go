package adapter

import (
	"context"

	invoiceModel "example.test/domain/invoice/model"
	"example.test/domain/rent/model"
)

// Cancel is declared and never called: an edge is what the adapter does, not
// what its interface allows.
type InvoiceManagement interface {
	Issue(ctx context.Context, amount int) (*invoiceModel.Invoice, error)
	Cancel(ctx context.Context, id int) error
}

type invoiceService struct {
	invoices InvoiceManagement
}

func NewInvoiceService(invoices InvoiceManagement) *invoiceService {
	return &invoiceService{invoices: invoices}
}

func (s *invoiceService) Issue(ctx context.Context, contract *model.Contract) (int, error) {
	issued, err := s.invoices.Issue(ctx, contract.Id)
	if err != nil {
		return 0, err
	}
	return issued.Id, nil
}
