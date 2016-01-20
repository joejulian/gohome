package www

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"mime"
	"net/http"
	"sort"

	"github.com/gorilla/mux"
	"github.com/markdaws/gohome"
	"github.com/markdaws/gohome/cmd"
	"github.com/markdaws/gohome/log"
)

type Server interface {
	ListenAndServe(port string) error
}

type wwwServer struct {
	rootPath      string
	system        *gohome.System
	recipeManager *gohome.RecipeManager
	eventLogger   gohome.WSEventLogger
}

func NewServer(
	rootPath string,
	system *gohome.System,
	recipeManager *gohome.RecipeManager,
	eventLogger gohome.WSEventLogger) Server {
	return &wwwServer{
		rootPath:      rootPath,
		system:        system,
		recipeManager: recipeManager,
		eventLogger:   eventLogger,
	}
}

func (s *wwwServer) ListenAndServe(port string) error {

	r := mux.NewRouter()

	mime.AddExtensionType(".jsx", "text/jsx")
	mime.AddExtensionType(".woff", "application/font-woff")
	mime.AddExtensionType(".woff2", "application/font-woff2")
	mime.AddExtensionType(".eot", "application/vnd.ms-fontobject")
	cssHandler := http.FileServer(http.Dir(s.rootPath + "/assets/css/"))
	jsHandler := http.FileServer(http.Dir(s.rootPath + "/assets/js/"))
	fontHandler := http.FileServer(http.Dir(s.rootPath + "/assets/fonts/"))
	jsxHandler := http.FileServer(http.Dir(s.rootPath + "/assets/jsx/"))
	imageHandler := http.FileServer(http.Dir(s.rootPath + "/assets/images/"))

	// Websocket handler
	r.HandleFunc("/api/v1/events/ws", s.eventLogger.HTTPHandler())

	//TODO: Move api into separate http server
	r.HandleFunc("/api/v1/systems/{systemId}/scenes", apiScenesHandler(s.system)).Methods("GET")
	r.HandleFunc("/api/v1/systems/{systemId}/zones", apiZonesHandler(s.system)).Methods("GET")
	r.HandleFunc("/api/v1/systems/{systemId}/devices", apiDevicesHandler(s.system)).Methods("GET")

	r.HandleFunc("/api/v1/cookbooks", apiCookBooksHandler(s.recipeManager.CookBooks)).Methods("GET")
	r.HandleFunc("/api/v1/cookbooks/{id}", apiCookBookHandler(s.recipeManager.CookBooks)).Methods("GET")

	//TODO: Need System in the api path?
	r.HandleFunc("/api/v1/recipes", apiRecipesHandlerPost(s.system, s.recipeManager)).Methods("POST")
	r.HandleFunc("/api/v1/recipes/{id}", apiRecipeHandler(s.system, s.recipeManager)).Methods("POST")
	r.HandleFunc("/api/v1/recipes/{id}", apiRecipeHandlerDelete(s.system, s.recipeManager)).Methods("DELETE")
	r.HandleFunc("/api/v1/recipes", apiRecipesHandlerGet(s.system, s.recipeManager)).Methods("GET")

	//TODO: GET vs. POST
	r.HandleFunc("/api/v1/systems/{systemId}/zones/{id}", apiZoneHandler(s.system))

	//TODO: Make for POST only
	//TODO: Have GET version to see the currently active scenes
	r.HandleFunc("/api/v1/systems/{systemId}/scenes/active", apiActiveScenesHandler(s.system)).Methods("POST")

	sub := r.PathPrefix("/assets").Subrouter()
	//sub.Methods("GET")
	sub.Handle("/css/{filename}", http.StripPrefix("/assets/css/", cssHandler))
	sub.Handle("/js/{filename}", http.StripPrefix("/assets/js/", jsHandler))
	sub.Handle("/fonts/{filename}", http.StripPrefix("/assets/fonts/", fontHandler))
	sub.Handle("/jsx/{filename}", http.StripPrefix("/assets/jsx/", jsxHandler))
	sub.Handle("/images/{filename}", http.StripPrefix("/assets/images/", imageHandler))
	r.HandleFunc("/", rootHandler(s.rootPath))
	return http.ListenAndServe(port, r)
}

func rootHandler(rootPath string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, rootPath+"/assets/html/index.html")
	}
}

func apiRecipesHandlerPost(system *gohome.System, recipeManager *gohome.RecipeManager) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {

		body, err := ioutil.ReadAll(io.LimitReader(r.Body, 1024))
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var data map[string]interface{}
		if err = json.Unmarshal(body, &data); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		recipe, err := recipeManager.UnmarshalNewRecipe(data)
		if err != nil {
			errBad := err.(*gohome.ErrUnmarshalRecipe)
			w.WriteHeader(http.StatusBadRequest)
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			json.NewEncoder(w).Encode(struct {
				ParamID     string `json:"paramId"`
				ErrorType   string `json:"errorType"`
				Description string `json:"description"`
			}{
				ParamID:     errBad.ParamID,
				ErrorType:   errBad.ErrorType,
				Description: errBad.Description,
			})
			return
		}

		recipeManager.RegisterAndStart(recipe)
		err = system.Save(recipeManager)
		if err != nil {
			fmt.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(struct {
			ID string `json:"id"`
		}{ID: recipe.ID})
	}
}

func apiRecipeHandler(system *gohome.System, recipeManager *gohome.RecipeManager) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {

		body, err := ioutil.ReadAll(io.LimitReader(r.Body, 1024))
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var data struct {
			Enabled bool `json:"enabled"`
		}
		if err = json.Unmarshal(body, &data); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		recipeID := mux.Vars(r)["id"]
		recipe := recipeManager.RecipeByID(recipeID)
		if recipe == nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		err = recipeManager.EnableRecipe(recipe, data.Enabled)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		system.Save(recipeManager)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(struct{}{})
	}
}

func apiRecipeHandlerDelete(system *gohome.System, recipeManager *gohome.RecipeManager) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {

		recipeID := mux.Vars(r)["id"]
		recipe := recipeManager.RecipeByID(recipeID)
		if recipe == nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		err := recipeManager.DeleteRecipe(recipe)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
		}

		system.Save(recipeManager)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(struct{}{})
	}
}

func apiRecipesHandlerGet(system *gohome.System, recipeManager *gohome.RecipeManager) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {

		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		recipes := system.Recipes
		jsonRecipes := make(jsonRecipes, len(recipes))

		i := 0
		for _, recipe := range recipes {
			jsonRecipes[i] = jsonRecipe{
				ID:          recipe.ID,
				Name:        recipe.Name,
				Description: recipe.Description,
				Enabled:     recipe.Enabled(),
			}
			i++
		}
		sort.Sort(jsonRecipes)
		if err := json.NewEncoder(w).Encode(jsonRecipes); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}
}

func apiCookBooksHandler(cookBooks []*gohome.CookBook) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json;charset=UTF-8")

		type jsonCookBook struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
			LogoURL     string `json:"logoUrl"`
		}

		//TODO: Return in a consistent order
		jsonCookBooks := make([]jsonCookBook, len(cookBooks))
		for i, cookBook := range cookBooks {
			jsonCookBooks[i] = jsonCookBook{
				ID:          cookBook.ID,
				Name:        cookBook.Name,
				Description: cookBook.Description,
				LogoURL:     cookBook.LogoURL,
			}
		}
		if err := json.NewEncoder(w).Encode(jsonCookBooks); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}
}

func apiCookBookHandler(cookBooks []*gohome.CookBook) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json;charset=UTF-8")

		type jsonIngredient struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
			Type        string `json:"type"`
			Reference   string `json:"reference"`
		}
		type jsonTrigger struct {
			ID          string           `json:"id"`
			Name        string           `json:"name"`
			Description string           `json:"description"`
			Ingredients []jsonIngredient `json:"ingredients"`
		}
		type jsonAction struct {
			ID          string           `json:"id"`
			Name        string           `json:"name"`
			Description string           `json:"description"`
			Ingredients []jsonIngredient `json:"ingredients"`
		}
		type jsonCookBook struct {
			ID          string        `json:"id"`
			Name        string        `json:"name"`
			Description string        `json:"description"`
			LogoURL     string        `json:"logoUrl"`
			Triggers    []jsonTrigger `json:"triggers"`
			Actions     []jsonAction  `json:"actions"`
		}

		vars := mux.Vars(r)
		cbID := vars["id"]
		var found = false
		for _, c := range cookBooks {
			if c.ID != cbID {
				continue
			}

			jsonTriggers := make([]jsonTrigger, len(c.Triggers))
			for i, t := range c.Triggers {
				jsonTriggers[i] = jsonTrigger{
					ID:          t.Type(),
					Name:        t.Name(),
					Description: t.Description(),
					Ingredients: make([]jsonIngredient, len(t.Ingredients())),
				}

				for j, ing := range t.Ingredients() {
					jsonTriggers[i].Ingredients[j] = jsonIngredient{
						ID:          ing.ID,
						Name:        ing.Name,
						Description: ing.Description,
						Type:        ing.Type,
					}
				}
			}

			// for each trigger need to json all ingredients
			jsonActions := make([]jsonAction, len(c.Actions))
			for i, a := range c.Actions {
				jsonActions[i] = jsonAction{
					ID:          a.Type(),
					Name:        a.Name(),
					Description: a.Description(),
					Ingredients: make([]jsonIngredient, len(a.Ingredients())),
				}

				for j, ing := range a.Ingredients() {
					jsonActions[i].Ingredients[j] = jsonIngredient{
						ID:          ing.ID,
						Name:        ing.Name,
						Description: ing.Description,
						Type:        ing.Type,
					}
				}
			}

			// for each action need to json all ingredients
			jsonCookBook := jsonCookBook{
				ID:          c.ID,
				Name:        c.Name,
				Description: c.Description,
				LogoURL:     c.LogoURL,
				Triggers:    jsonTriggers,
				Actions:     jsonActions,
			}
			if err := json.NewEncoder(w).Encode(jsonCookBook); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
			}
			found = true
			break
		}

		if !found {
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func apiScenesHandler(system *gohome.System) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json;charset=UTF-8")

		scenes := make(scenes, len(system.Scenes), len(system.Scenes))
		var i int32 = 0
		for _, scene := range system.Scenes {
			scenes[i] = jsonScene{ID: scene.GlobalID, Name: scene.Name, Description: scene.Description}
			i++
		}
		sort.Sort(scenes)
		if err := json.NewEncoder(w).Encode(scenes); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}
}

func apiZonesHandler(system *gohome.System) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=UTF-8")

		zones := make(zones, len(system.Zones), len(system.Zones))
		var i int32 = 0
		for _, zone := range system.Zones {
			zones[i] = jsonZone{
				ID:          zone.GlobalID,
				Name:        zone.Name,
				Description: zone.Description,
				Type:        zone.Type.ToString(),
				Output:      zone.Output.ToString(),
			}
			i++
		}
		sort.Sort(zones)
		if err := json.NewEncoder(w).Encode(zones); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}
}

func apiDevicesHandler(system *gohome.System) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=UTF-8")

		devices := make(devices, len(system.Devices), len(system.Devices))
		var i int32 = 0
		for _, device := range system.Devices {
			devices[i] = jsonDevice{
				LocalID:     device.LocalID(),
				GlobalID:    device.GlobalID(),
				Name:        device.Name(),
				Description: device.Description(),
				ModelNumber: device.ModelNumber(),
			}
			i++
		}
		sort.Sort(devices)
		if err := json.NewEncoder(w).Encode(devices); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}
}

func apiZoneHandler(system *gohome.System) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := ioutil.ReadAll(io.LimitReader(r.Body, 1024))
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		var x struct {
			Value float32 `json:"value"`
		}
		if err = json.Unmarshal(body, &x); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		vars := mux.Vars(r)
		zone, ok := system.Zones[vars["id"]]
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		err = system.CmdProcessor.Enqueue(&cmd.ZoneSetLevel{
			ZoneLocalID:  zone.LocalID,
			ZoneGlobalID: zone.GlobalID,
			ZoneName:     zone.Name,
			Level:        x.Value,
		})
		/*
			err = zone.Device.Enqueue(&gohome.ZoneSetLevelCommand{
				Zone:  zone,
				Level: x.Value,
			})*/
		//TODO: Remove
		//err = zone.SetLevel(x.Value)
		if err != nil {
			log.W("enqueue zone set level failed: %s", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(struct{}{})
	}
}

func apiActiveScenesHandler(system *gohome.System) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := ioutil.ReadAll(io.LimitReader(r.Body, 1024))
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		var x struct {
			Id string `json:"id"`
		}
		if err = json.Unmarshal(body, &x); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		scene, ok := system.Scenes[x.Id]
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		err = system.CmdProcessor.Enqueue(&cmd.SceneSet{
			SceneGlobalID: scene.GlobalID,
			SceneName:     scene.Name,
		})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(struct{}{})
	}
}
