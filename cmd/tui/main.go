package main

import (
	"log"

	"github.com/kungfusheep/glyph"
)

func main() {
	app := glyph.NewInlineApp()
	if err := app.SetView(
		glyph.VBox(
			glyph.Text("conficius"),
			glyph.On(
				glyph.Key("<Enter>", app.Stop),
			),
		),
	).
		ClearOnExit(true).
		Run(); err != nil {
		log.Fatal(err)
	}
}
