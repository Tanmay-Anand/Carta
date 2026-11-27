package survey

import (
	"github.com/Tanmay-Anand/carta/internal/model"
)

// kind is how a device is drawn: the four states the legend names. The rules
// are the web UI's (internal/ui/assets/app.js kindOf), so a survey looks the
// same in the browser, on the phone and here.
type kind int

const (
	kindNamed   kind = iota // a name and a confident identification
	kindPartial             // one of the two
	kindUnknown             // neither: answered, but said nothing useful
	kindInfra               // the gateway and the instrument itself
)

func kindOf(e *model.Entity) kind {
	class, classConf := e.Best("class")
	host, _ := e.Best("hostname")
	if class == "router / gateway" || class == "survey vantage" {
		return kindInfra
	}
	// A weak class is a guess, not an identification, and drawing it with the
	// weight of a device that announced its own model is the flattening this
	// tool exists to avoid.
	identified := class != "" && classConf > model.Low
	switch {
	case host == "" && !identified:
		return kindUnknown
	case host == "" || !identified:
		return kindPartial
	}
	return kindNamed
}

// sublabel is what a device is, with guesses marked as guesses.
func sublabel(e *model.Entity) string {
	class, classConf := e.Best("class")
	vendor, vendorConf := e.Best("vendor")
	switch {
	case class != "" && classConf > model.Low:
		return class
	case class != "":
		return class + "?"
	case vendor != "" && vendorConf > model.Moderate:
		return vendor
	case vendor != "":
		return vendor + "?"
	}
	return "unidentified"
}

func isAlive(e *model.Entity) bool {
	v, _ := e.Best("alive")
	return v == "yes"
}
