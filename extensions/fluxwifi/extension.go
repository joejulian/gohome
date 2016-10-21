package fluxwifi

import (
	"github.com/markdaws/gohome"
	"github.com/markdaws/gohome/cmd"
)

type extension struct {
}

func (e *extension) RegisterCmdBuilders(sys *gohome.System, lookupTable map[string]cmd.Builder) {
	builder := &cmdBuilder{System: sys}
	lookupTable[builder.ID()] = builder
}

func (e *extension) RegisterNetwork(sys *gohome.System, lookupTable map[string]gohome.Network) {
	lookupTable["fluxwifi"] = &network{System: sys}
}

func (e *extension) RegisterImporters(sys *gohome.System, lookupTable map[string]gohome.Importer) {
}

func NewExtension() *extension {
	return &extension{}
}
