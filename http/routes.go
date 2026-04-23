package http

import (
	"log/slog"

	"maragu.dev/glue/http"

	"app/service"
)

func InjectHTTPRouter(log *slog.Logger, svc *service.Fat) func(*Router) {
	return func(r *Router) {
		r.Group(func(r *http.Router) {
			Search(r, log, searchServiceAdapter{searchDB: svc.DB(), queue: svc.Queue()}, svc.ImageStore(), svc.LLM())
		})
	}
}
