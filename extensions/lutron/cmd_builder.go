package lutron

import (
	"fmt"
	"io"
	"time"

	lutronExt "github.com/go-home-iot/lutron"
	"github.com/markdaws/gohome"
	"github.com/markdaws/gohome/cmd"
)

type cmdBuilder struct {
	System *gohome.System
	device lutronExt.Device
}

func (b *cmdBuilder) Build(c cmd.Command) (*cmd.Func, error) {

	if b.device == nil {
		lDev, err := lutronExt.DeviceFromModelNumber(b.ID())
		if err != nil {
			return nil, err
		}
		b.device = lDev
	}

	switch command := c.(type) {
	case *cmd.ZoneSetLevel:
		return &cmd.Func{
			Func: func() error {
				z := b.System.Zones[command.ZoneID]
				dev := b.System.Devices[z.DeviceID]
				return getWriterAndExec(dev, func(w io.Writer) error {
					return b.device.SetLevel(command.Level.Value, command.ZoneAddress, w)
				})
			},
		}, nil
	case *cmd.ZoneTurnOn:
		return &cmd.Func{
			Func: func() error {
				z := b.System.Zones[command.ZoneID]
				dev := b.System.Devices[z.DeviceID]
				return getWriterAndExec(dev, func(w io.Writer) error {
					return b.device.SetLevel(100.0, command.ZoneAddress, w)
				})
			},
		}, nil
	case *cmd.ZoneTurnOff:
		return &cmd.Func{
			Func: func() error {
				z := b.System.Zones[command.ZoneID]
				dev := b.System.Devices[z.DeviceID]
				return getWriterAndExec(dev, func(w io.Writer) error {
					return b.device.SetLevel(0.0, command.ZoneAddress, w)
				})
			},
		}, nil
	case *cmd.ButtonPress:
		return &cmd.Func{
			Func: func() error {
				dev := b.System.Devices[command.DeviceID]
				return getWriterAndExec(dev, func(w io.Writer) error {
					return b.device.ButtonPress(command.DeviceAddress, command.ButtonAddress, w)
				})
			},
		}, nil
	case *cmd.ButtonRelease:
		return &cmd.Func{
			Func: func() error {
				dev := b.System.Devices[command.DeviceID]
				return getWriterAndExec(dev, func(w io.Writer) error {
					return b.device.ButtonPress(command.DeviceAddress, command.ButtonAddress, w)
				})
			},
		}, nil

	default:
		return nil, fmt.Errorf("unsupported command type")
	}
	return nil, nil
}

func (b *cmdBuilder) ID() string {
	return "l-bdgpro2-wh"
}

func getWriterAndExec(d *gohome.Device, f func(io.Writer) error) error {
	var hub *gohome.Device = d
	if d.Hub != nil {
		hub = d.Hub
	}

	conn, err := hub.Connections.Get(time.Second * 5)
	if err != nil {
		return fmt.Errorf("error connecting, pool returned err: %s", err)
	}

	defer func() {
		hub.Connections.Release(conn)
	}()

	err = f(conn)
	if err != nil {
		return fmt.Errorf("Failed to send command %s\n", err)
	}
	return nil
}
