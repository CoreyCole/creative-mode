package main

import (
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	l "github.com/coreycole/creative-mode/site/layouts"
	p "github.com/coreycole/creative-mode/site/pages"
)

const port = "3000"

func main() {
	e := echo.New()

	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORS())

	e.GET("/", func(c echo.Context) error {
		rootArgs := l.RootArgs{
			Title:       "Creative Mode",
			CurrentPath: c.Request().URL.Path,
		}
		component := p.HomePage(rootArgs)
		return component.Render(c.Request().Context(), c.Response().Writer)
	})

	// Serve static files
	e.Static("/", "static/")

	if err := e.Start(":" + port); err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
