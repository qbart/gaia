package web

import (
	"context"
	"embed"
	"log/slog"
	"net/http"
	"os"

	"github.com/SoftKiwiGames/zen/zen"
	"github.com/qbart/gaia/config"
	"github.com/qbart/gaia/web/ui"
)

//go:embed static
var static embed.FS

type Server struct {
	Envs zen.Envs
}

func (s *Server) Run(ctx context.Context) {
	embeds, err := zen.NewEmbeds(static, "static", zen.ReactPreset)
	if err != nil {
		slog.Error("embedding failed", "err", err.Error())
		os.Exit(1)
	}

	srv := zen.NewHttpServer(&zen.Options{
		AllowedHosts: config.AllowedHosts(),
		CorsOrigins:  config.AllowedOrigins(),
		SSL:          config.SSL,
	})
	srv.Embeds("/static", embeds)
	srv.Get("/", func(w http.ResponseWriter, r *http.Request) {
		ui.Layout(ui.LayoutPage{}, nil).Render(r.Context(), w)
	})

	s.Envs["ADDR"] = ":4000"
	srv.Run(ctx, s.Envs)
}
