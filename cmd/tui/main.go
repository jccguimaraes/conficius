package main

import (
	. "github.com/kungfusheep/glyph"
)

func main() {
	app := NewInlineApp()
	app.SetView(
		VBox(
			Text("conficius"),
			On(
				Key("<Enter>", app.Stop),
			),
		),
	).
		ClearOnExit(true).
		Run()
}
