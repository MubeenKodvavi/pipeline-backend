package image

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"math"

	"google.golang.org/protobuf/types/known/structpb"

	"github.com/instill-ai/pipeline-backend/pkg/component/base"
)

type cropInput struct {
	Image        base64Image `json:"image"`
	CornerRadius int         `json:"corner-radius"`
	CircleRadius int         `json:"circle-radius"`
	TopOffset    int         `json:"top-offset"`
	RightOffset  int         `json:"right-offset"`
	BottomOffset int         `json:"bottom-offset"`
	LeftOffset   int         `json:"left-offset"`
}

type cropOutput struct {
	Image base64Image `json:"image"`
}

func cropCornerRadius(img image.Image, radius int) image.Image {
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	result := image.NewRGBA(bounds)

	radiusSquared := radius * radius

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if isInsideRoundedCorner(x, y, width, height, radius, radiusSquared) {
				result.Set(x, y, img.At(x, y))
			} else {
				result.Set(x, y, color.Transparent)
			}
		}
	}

	return result
}

func isInsideRoundedCorner(x, y, width, height, radius int, radiusSquared int) bool {
	dx, dy := 0, 0
	switch {
	case x < radius && y < radius: // Top-left corner
		dx, dy = radius-x-1, radius-y-1
	case x >= width-radius && y < radius: // Top-right corner
		dx, dy = x-(width-radius), radius-y-1
	case x < radius && y >= height-radius: // Bottom-left corner
		dx, dy = radius-x-1, y-(height-radius)
	case x >= width-radius && y >= height-radius: // Bottom-right corner
		dx, dy = x-(width-radius), y-(height-radius)
	default:
		return true
	}
	return dx*dx+dy*dy < radiusSquared
}

func cropCircle(img image.Image, centerX, centerY, radius int) image.Image {
	bounds := img.Bounds()
	result := image.NewRGBA(bounds)
	radiusSquared := radius * radius

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			dx, dy := x-centerX, y-centerY
			if dx*dx+dy*dy <= radiusSquared {
				result.Set(x, y, img.At(x, y))
			} else {
				result.Set(x, y, color.Transparent)
			}
		}
	}

	return result
}

func crop(input *structpb.Struct, job *base.Job, ctx context.Context) (*structpb.Struct, error) {
	inputStruct := cropInput{}

	err := base.ConvertFromStructpb(input, &inputStruct)
	if err != nil {
		return nil, fmt.Errorf("error converting input to struct: %v", err)
	}

	img, err := decodeBase64Image(string(inputStruct.Image))
	if err != nil {
		return nil, fmt.Errorf("error decoding image: %v", err)
	}

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// Always perform rectangular crop first
	x1 := inputStruct.LeftOffset
	y1 := inputStruct.TopOffset
	x2 := width - inputStruct.RightOffset
	y2 := height - inputStruct.BottomOffset

	if x1 < 0 || y1 < 0 || x2 > width || y2 > height || x1 >= x2 || y1 >= y2 {
		return nil, fmt.Errorf("invalid crop dimensions")
	}

	// Create a new image with the cropped dimensions
	croppedImg := image.NewRGBA(image.Rect(0, 0, x2-x1, y2-y1))

	// Copy the pixels from the original image to the new cropped image
	for y := y1; y < y2; y++ {
		for x := x1; x < x2; x++ {
			croppedImg.Set(x-x1, y-y1, img.At(x, y))
		}
	}

	// Apply corner radius or circle crop if specified
	if inputStruct.CircleRadius > 0 {
		bounds := croppedImg.Bounds()
		width, height := bounds.Dx(), bounds.Dy()
		centerX, centerY := width/2, height/2
		radius := inputStruct.CircleRadius

		// Limit radius to half of the smaller dimension
		maxRadius := width
		if height < width {
			maxRadius = height
		}
		if radius > maxRadius/2 {
			radius = maxRadius / 2
		}

		croppedImg = cropCircle(croppedImg, centerX, centerY, radius).(*image.RGBA)
	} else if inputStruct.CornerRadius > 0 {
		bounds := croppedImg.Bounds()
		width, height := bounds.Dx(), bounds.Dy()

		// Limit corner radius to half of the smaller dimension
		maxRadius := math.Min(float64(width), float64(height)) / 2
		radius := int(math.Min(float64(inputStruct.CornerRadius), maxRadius))
		croppedImg = cropCornerRadius(croppedImg, radius).(*image.RGBA)
	}

	base64Img, err := encodeBase64Image(croppedImg)
	if err != nil {
		return nil, err
	}

	output := cropOutput{
		Image: base64Image(fmt.Sprintf("data:image/png;base64,%s", base64Img)),
	}

	return base.ConvertToStructpb(output)
}