package catalog

import (
	"image"
	"image/color"
	"testing"
)

// The beads sit in the lower left; the rest of the picture and a few glints and
// shadows must not move the colour.
func TestPhotoColorTakesTheMedianOfTheLowerLeft(t *testing.T) {
	photo := image.NewRGBA(image.Rect(0, 0, 100, 100))
	for y := range 100 {
		for x := range 100 {
			c := color.RGBA{255, 255, 255, 255}
			if x < 50 && y >= 50 {
				c = color.RGBA{10, 120, 60, 255}
				switch (x + y) % 10 {
				case 0, 1, 2:
					c = color.RGBA{0, 0, 0, 255}
				case 3:
					c = color.RGBA{250, 250, 250, 255}
				}
			}
			photo.Set(x, y, c)
		}
	}
	if got := PhotoColor(photo); got != "#0A783C" {
		t.Errorf("PhotoColor = %s, want #0A783C", got)
	}
}
