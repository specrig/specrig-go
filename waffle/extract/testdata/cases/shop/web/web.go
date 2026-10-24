package web

import (
	"context"
	"fmt"

	"gitlab.com/gorib/http/client"
	"gitlab.com/gorib/http/server"
)

type router struct{ renderer *renderer }

func (r *router) Setup(mux server.Mux) {
	mux.Get("/health", r.document)
	r.admin(mux)
	mux.Group("/customers").Post("/password")
}

func (r *router) admin(mux server.Mux) {
	a := mux.Group("/admin")
	a.Get("/users/{userId}")
	a.Handle("DELETE", "/users/{userId}")
}

func (r *router) document(v any) any {
	_ = r.renderer.render(context.Background(), 1)
	return v
}

type renderer struct{ c *client.Client }

func (r *renderer) render(ctx context.Context, id int) error {
	if _, err := r.c.Post(ctx, "/api/render/text"); err != nil {
		return err
	}
	_, err := r.c.Get(ctx, fmt.Sprintf("/api/documents/%d?full=%t", id, true))
	return err
}

type asset struct{ Url string }

func (r *renderer) download(ctx context.Context, a asset) error {
	_, err := r.c.Get(ctx, a.Url)
	return err
}
