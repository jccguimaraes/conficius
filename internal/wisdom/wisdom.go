package wisdom

import "math/rand/v2"

func Random() string {
	phrases := []string{
		"A journey of a thousand updates begins with a single backup.",
		"To know what you have installed, and what you have not installed, that is true knowledge.",
		"The wise developer seeks the bug within; the foolish developer blames the package manager.",
		"It does not matter how slowly you update, as long as you do not stay on the vulnerable version.",
		"When you see a good dotfile, think of linking it. When you see a bad dotfile, examine your own.",
		"To make a mistake and not roll it back is the real mistake.",
		"The developer who moves a mountain begins by removing unused dependencies.",
		"Is it not a pleasure to learn, and then run --help again?",
	}
	return "Conficius says: " + phrases[rand.IntN(len(phrases))]
}
