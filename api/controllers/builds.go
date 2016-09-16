package controllers

import (
	"bytes"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/convox/rack/api/httperr"
	"github.com/convox/rack/api/models"
	"github.com/convox/rack/api/structs"
	"github.com/gorilla/mux"
	"golang.org/x/net/websocket"
)

func BuildCreate(rw http.ResponseWriter, r *http.Request) *httperr.Error {
	vars := mux.Vars(r)
	app := vars["app"]

	opts := structs.BuildOptions{
		Cache:       !(r.FormValue("cache") == "false"),
		Config:      r.FormValue("config"),
		Description: r.FormValue("description"),
	}

	if r.FormValue("import") != "" {
		return httperr.Errorf(403, "endpoint deprecated, please update your client")
	}

	image, _, err := r.FormFile("image")
	if err != nil && err != http.ErrMissingFile && err != http.ErrNotMultipart {
		return httperr.Server(err)
	}
	if image != nil {
	}

	source, _, err := r.FormFile("source")
	if err != nil && err != http.ErrMissingFile && err != http.ErrNotMultipart {
		return httperr.Server(err)
	}
	if source != nil {
		url, err := models.Provider().ObjectStore("", source, structs.ObjectOptions{})
		if err != nil {
			return httperr.Server(err)
		}

		build, err := models.Provider().BuildCreate(app, "tgz", url, opts)
		if err != nil {
			return httperr.Server(err)
		}

		return RenderJson(rw, build)
	}

	if index := r.FormValue("index"); index != "" {
		url, err := models.Provider().ObjectStore("", bytes.NewReader([]byte(index)), structs.ObjectOptions{})
		if err != nil {
			return httperr.Server(err)
		}

		build, err := models.Provider().BuildCreate(app, "index", url, opts)
		if err != nil {
			return httperr.Server(err)
		}

		return RenderJson(rw, build)
	}

	// TODO deprecate
	if repo := r.FormValue("repo"); repo != "" {
	}

	if url := r.FormValue("url"); url != "" {
		method := ""
		ext := filepath.Ext(url)

		switch ext {
		case ".git":
			method = "git"
		case ".tgz":
			method = "tgz"
		case ".zip":
			method = "zip"
		default:
			return httperr.Errorf(403, "unknown extension: %s", ext)
		}

		build, err := models.Provider().BuildCreate(app, method, url, opts)
		if err != nil {
			return httperr.Server(err)
		}

		return RenderJson(rw, build)
	}

	return httperr.Errorf(403, "no build source found")
}

// BuildDelete deletes a build. Makes sure not to delete a build that is contained in the active release
func BuildDelete(rw http.ResponseWriter, r *http.Request) *httperr.Error {
	vars := mux.Vars(r)
	appName := vars["app"]
	buildID := vars["build"]

	err := models.Provider().ReleaseDelete(appName, buildID)
	if err != nil {
		return httperr.Server(err)
	}

	build, err := models.Provider().BuildDelete(appName, buildID)
	if err != nil {
		return httperr.Server(err)
	}

	return RenderJson(rw, build)
}

// BuildExport creates an artifact, representing a build, to be used with another Rack
func BuildExport(rw http.ResponseWriter, r *http.Request) *httperr.Error {
	vars := mux.Vars(r)
	app := vars["app"]
	build := vars["build"]

	b, err := models.Provider().BuildGet(app, build)
	if awsError(err) == "ValidationError" {
		return httperr.Errorf(404, "no such app: %s", app)
	}
	if err != nil && strings.HasPrefix(err.Error(), "no such build") {
		return httperr.Errorf(404, err.Error())
	}
	if err != nil {
		return httperr.Server(err)
	}

	rw.Header().Set("Content-Type", "application/octet-stream")

	if err = models.Provider().BuildExport(app, b.Id, rw); err != nil {
		return httperr.Server(err)
	}

	return nil
}

func BuildGet(rw http.ResponseWriter, r *http.Request) *httperr.Error {
	vars := mux.Vars(r)
	app := vars["app"]
	build := vars["build"]

	b, err := models.Provider().BuildGet(app, build)
	if awsError(err) == "ValidationError" {
		return httperr.Errorf(404, "no such app: %s", app)
	}
	if err != nil && strings.HasPrefix(err.Error(), "no such build") {
		return httperr.Errorf(404, err.Error())
	}
	if err != nil {
		return httperr.Server(err)
	}

	return RenderJson(rw, b)
}

func BuildList(rw http.ResponseWriter, r *http.Request) *httperr.Error {
	app := mux.Vars(r)["app"]

	l := r.URL.Query().Get("limit")

	var err error
	var limit int

	if l == "" {
		limit = 20
	} else {
		limit, err = strconv.Atoi(l)
		if err != nil {
			return httperr.Errorf(400, err.Error())
		}
	}

	builds, err := models.Provider().BuildList(app, int64(limit))
	if awsError(err) == "ValidationError" {
		return httperr.Errorf(404, "no such app: %s", app)
	}
	if err != nil {
		return httperr.Server(err)
	}

	return RenderJson(rw, builds)
}

func BuildLogs(ws *websocket.Conn) *httperr.Error {
	vars := mux.Vars(ws.Request())

	app := vars["app"]
	build := vars["build"]

	if err := models.Provider().BuildLogs(app, build, ws); err != nil {
		return httperr.Server(err)
	}

	return nil
}
