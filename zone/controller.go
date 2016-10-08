package zone

// TODO: This should not be a type - want to eventually be able to add new
// device support without having to modify any code.  New devices should
// be configurable via some JSON format
type Controller string

const (
	ZCDefault  Controller = ""
	ZCFluxWIFI            = "FluxWIFI"
)

func ControllerFromString(c string) Controller {
	switch c {
	case "FluxWIFI":
		return ZCFluxWIFI
	default:
		return ZCDefault
	}
}
