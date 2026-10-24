//go:generate diogen -output config/diogen/di_gen.go $GOFILE

package di

import (
	invoiceService "example.test/domain/invoice/service"
	"example.test/domain/rent/adapter"

	"gitlab.com/gorib/di"
)

// Only rent is handed invoice's service; merchandize's adapter asks for the
// same shape and gets nothing from the container.
func InitDi() {
	di.Define(invoiceService.NewInvoices,
		di.Alias[adapter.InvoiceManagement](),
	)
}
