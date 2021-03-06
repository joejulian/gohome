package www

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/markdaws/gohome/pkg/gohome"
	errExt "github.com/pkg/errors"
)

// RegisterDiscoveryHandlers registers all of the discovery specific API REST routes
func RegisterDiscoveryHandlers(r *mux.Router, s *Server) {

	// Get a list of all the devices that we can discover
	r.HandleFunc("/v1/discovery/discoverers",
		apiListDiscoveryHandler(s.system)).Methods("GET")

	// Scan the network for all devices corresponding to the discovery ID
	r.HandleFunc("/v1/discovery/discoverers/{discovererID}",
		apiDiscoveryHandler(s.system)).Methods("POST")
}

func apiListDiscoveryHandler(system *gohome.System) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {

		infos := system.Extensions.ListDiscoverers(system)

		jsonInfos := make([]jsonDiscovererInfo, len(infos))
		for i, info := range infos {
			jsonUIFields := make([]jsonUIField, len(info.UIFields))
			for j, field := range info.UIFields {
				jsonUIFields[j] = jsonUIField{
					ID:          field.ID,
					Label:       field.Label,
					Description: field.Description,
					Default:     field.Default,
					Required:    field.Required,
				}
			}

			jsonInfos[i] = jsonDiscovererInfo{
				ID:          info.ID,
				Name:        info.Name,
				Description: info.Description,
				PreScanInfo: info.PreScanInfo,
				UIFields:    jsonUIFields,
			}
		}
		if err := json.NewEncoder(w).Encode(jsonInfos); err != nil {
			respErr(errExt.Wrap(err, "failed to encode JSON"), w)
		}
	}
}

func writeDiscoveryResults(sys *gohome.System, result *gohome.DiscoveryResults, w http.ResponseWriter) {
	// Need to serialize the scenes, use handy functions from scenes.go
	inputScenes := make(map[string]*gohome.Scene)
	for _, scene := range result.Scenes {
		inputScenes[scene.ID] = scene
	}

	inputDevices := make(map[string]*gohome.Device)
	dupeDevices := make(map[string]*gohome.Device)
	deviceToDupe := make(map[string]*gohome.Device)

	// Given the discovery results, we search the existing system to see if the discovery results
	// are returning any duplicate device/zone/sensor entries, if so we mark them appropriately
	// and return the existing devices to the user, with any new devices/zones/sensors appended

	for _, device := range result.Devices {
		// We are using Hub as an equality check, but in the case where we have discovered items on
		// the network which were previously imported, we now have to go through the discovery
		// information and find all the potential hub devices (items with Hub == nil) then loop again
		// and find all of the devices where Hub != nil and see if we have to replace those hub references
		// with references to already imported devices- phew
		if device.Hub != nil {
			continue
		}

		if dupeDevice, isDupe := sys.IsDupeDevice(device); isDupe {
			dupeDevices[dupeDevice.ID] = device
			deviceToDupe[device.ID] = dupeDevice
		} else {
			inputDevices[device.ID] = device
		}
	}

	// Loop again patching all of the Hub references with any items that were already imported
	for _, device := range result.Devices {
		if device.Hub == nil {
			continue
		}

		//Need to fix the hubID if it points to a dupe device
		if dupeDevice, ok := deviceToDupe[device.Hub.ID]; ok {
			device.Hub = dupeDevice
		}

		if dupeDevice, isDupe := sys.IsDupeDevice(device); isDupe {
			dupeDevices[dupeDevice.ID] = device
		} else {
			inputDevices[device.ID] = device
		}
	}

	// JSONify all the non dupe devices
	jsonDevices := DevicesToJSON(inputDevices)

	// For all the devices we found that were dupes, we need to JSONify those separately
	// along with merging the zones + sensors of the current discovery with zones/sensors
	// already attached to the device.  For example the user may have already imported a
	// device and zone previously, then added a new zone and done a rescan, we need to
	// return the existing device and zone but also append the new zone so the user has
	// change to import the new zone
	for existingDeviceID, dupeDevice := range dupeDevices {
		existingDevice := sys.DeviceByID(existingDeviceID)

		// JSONify the existing device, since this is a dupe we want to send back the
		// current device to the user
		jsonDupeDevice := DevicesToJSON(map[string]*gohome.Device{existingDevice.ID: existingDevice})[0]
		jsonDupeDevice.IsDupe = true

		// Mark all features as dupes since they are already in the system
		features := jsonDupeDevice.Features
		for _, f := range features {
			f.IsDupe = true
		}

		// Loop through the new items returned from the discovery results, see which ones
		// are dupes and non dupes
		for _, feature := range dupeDevice.Features {
			isDupe := existingDevice.IsDupeFeature(feature)
			feature.IsDupe = isDupe
			feature.DeviceID = existingDevice.ID
			jsonDupeDevice.Features = append(jsonDupeDevice.Features, feature)
		}

		jsonDevices = append(jsonDevices, jsonDupeDevice)
	}

	jsonScenes := ScenesToJSON(inputScenes)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(struct {
		Devices []jsonDevice `json:"devices"`
		Scenes  []jsonScene  `json:"scenes"`
	}{
		Devices: jsonDevices,
		Scenes:  jsonScenes,
	}); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func apiDiscoveryHandler(sys *gohome.System) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {

		vars := mux.Vars(r)
		discovererID := vars["discovererID"]

		discoverer := sys.Extensions.FindDiscovererFromID(sys, discovererID)
		if discoverer == nil {
			respBadRequest(fmt.Sprintf("unknown discoverer id %s", discovererID), w)
			return
		}

		body, err := ioutil.ReadAll(io.LimitReader(r.Body, 1024*1024))
		if err != nil {
			respBadRequest("request body too large, max 1MB", w)
			return
		}

		var uiFields map[string]string
		info := discoverer.Info()
		if len(info.UIFields) > 0 {
			if err := json.Unmarshal(body, &uiFields); err != nil {
				respBadRequest(fmt.Sprintf("error unmarshaling uiFields %s", err), w)
				return
			}

			for _, uiField := range info.UIFields {
				if uiField.Required && uiFields[uiField.ID] == "" {
					respBadRequest(fmt.Sprintf("missing required field: '%s'", uiField.Label), w)
					return
				}
			}
		}

		res, err := discoverer.ScanDevices(sys, uiFields)
		if err != nil {
			respErr(err, w)
			return
		}

		writeDiscoveryResults(sys, res, w)
	}
}
