package scribus

type Handler interface {
	Run(src any) error
	Keys() []string
}

type BatchHandler interface {
	Run(msgs []any) error
	Keys() []string
}
