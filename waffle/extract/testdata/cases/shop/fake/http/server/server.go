package server

type Layer = func(any) any

type Mux interface {
	Group(prefix string, layers ...Layer) Mux
	Get(path string, layers ...Layer)
	Post(path string, layers ...Layer)
	Handle(method, path string, layers ...Layer)
}

type Router interface {
	Setup(mux Mux)
}
